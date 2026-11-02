package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
)

// Native DTOs deliberately have no App/service fields. A native stage remains
// bound to its retained execution rather than acquiring a synthetic App.
type NativeRuntimeLogReplicas struct {
	Subject  NativeRuntimeSubject `json:"subject"`
	RunID    string               `json:"runId"`
	Replicas []RuntimeLogReplica  `json:"replicas"`
}

type NativeRuntimeLogPage struct {
	Subject          NativeRuntimeSubject `json:"subject"`
	RunID            string               `json:"runId"`
	SessionID        string               `json:"sessionId"`
	ContainerID      string               `json:"containerId"`
	Since            time.Time            `json:"since"`
	Until            time.Time            `json:"until"`
	ExpiresAt        time.Time            `json:"expiresAt"`
	Offset           int                  `json:"offset"`
	NextOffset       *int                 `json:"nextOffset,omitempty"`
	Total            int                  `json:"total"`
	TailLimitReached bool                 `json:"tailLimitReached"`
	Coverage         string               `json:"coverage"`
	Entries          []RuntimeLogEntry    `json:"entries"`
}

// Only safe selected metadata enters the cache. Values, full configuration,
// accepted queue payload and private redaction contexts never enter it.
type nativeRuntimeLogMetadata struct {
	Subject         NativeRuntimeSubject   `json:"subject"`
	PipelineVersion int                    `json:"pipelineVersion"`
	Receipt         model.RuntimeReceipt   `json:"receipt"`
	Generation      *EnvironmentSecretPlan `json:"generation"`
}

func cloneNativeStageSecretPin(pin *EnvironmentSecretPlan, keys []string) *EnvironmentSecretPlan {
	if pin == nil {
		return nil
	}
	copy := *pin
	copy.Keys = slices.Clone(keys)
	sort.Strings(copy.Keys)
	copy.References = nil
	if pin.Mode == "external" {
		copy.References = make(map[string]secrets.VersionReference, len(copy.Keys))
		for _, key := range copy.Keys {
			copy.References[key] = pin.References[key]
		}
	}
	return &copy
}

func nativeLogMetadata(b *nativeRuntimeBinding) *nativeRuntimeLogMetadata {
	var pin *EnvironmentSecretPlan
	if b.secretPin != nil {
		pin = cloneNativeStageSecretPin(b.secretPin, b.secretPin.Keys)
	}
	return &nativeRuntimeLogMetadata{Subject: b.subject, PipelineVersion: b.pipelineVersion, Receipt: b.receipt, Generation: pin}
}

func nativeLogSubject(s NativeRuntimeSubject) (NativeRuntimeSubject, error) {
	if s.Kind == "" {
		s.Kind = "native-stage"
	}
	if s.Kind != "native-stage" || !nativeRuntimeID(s.PipelineID) || !nativeRuntimeID(s.RunID) || !nativeRuntimeID(s.StageID) {
		return s, ErrRuntimeLogRequest
	}
	return s, nil
}

func nativeLogError(err error) error {
	switch {
	case errors.Is(err, ErrNativeRuntimeRequest):
		return ErrRuntimeLogRequest
	case errors.Is(err, ErrNativeRuntimeNotFound):
		return ErrRuntimeLogExpired
	default:
		return ErrRuntimeLogUnavailable
	}
}

func (r *RuntimeService) nativeStageLogBinding(ctx context.Context, s NativeRuntimeSubject, jobs jobqueue.Store) (*nativeRuntimeBinding, error) {
	s, err := nativeLogSubject(s)
	if err != nil {
		return nil, err
	}
	b, _, err := r.resolveNativeStage(ctx, s, jobs)
	if err != nil {
		return nil, nativeLogError(err)
	}
	if b == nil {
		return nil, ErrRuntimeLogUnavailable
	}
	return b, nil
}

// Full accepted plan validation happens in resolveNativeStage first. This
// method resolves only the selected stage's cloned subset of that union pin.
// The explicit no-reference case performs no environment or manager reads.
func nativeStageLogSecrets(ctx context.Context, b *nativeRuntimeBinding, resolver *AppEnvResolver) ([]string, error) {
	if b.secretPin == nil {
		return []string{}, nil
	}
	if resolver == nil || resolver.Environments == nil || resolver.Secrets == nil {
		return nil, ErrRuntimeLogUnavailable
	}
	pin := b.secretPin
	ctx = WithAcceptedSecretPlan(WithEnvironmentVersion(ctx, pin.EnvironmentID, pin.EnvironmentVersion), pin)
	env, err := resolver.Environments.Get(ctx, pin.EnvironmentID)
	if err != nil || env == nil || env.ID != pin.EnvironmentID || env.Version != pin.EnvironmentVersion || model.IsRegistryCredentialEnvironmentID(env.ID) || model.IsRegistryCredentialEnvironment(env) {
		return nil, ErrRuntimeLogUnavailable
	}
	keys, accepted, pinned, err := resolver.acceptedSecretKeys(ctx, env)
	if err != nil || !pinned || accepted == nil || !slices.Equal(keys, pin.Keys) {
		return nil, ErrRuntimeLogUnavailable
	}
	values := []string{}
	for _, key := range keys {
		var value []byte
		if pin.Mode == "external" {
			value, err = secrets.ReadPinned(ctx, resolver.Secrets, env.ID, key, pin.References[key])
		} else {
			value, err = resolver.Secrets.Get(ctx, env.ID, key)
		}
		if err != nil {
			return nil, ErrRuntimeLogUnavailable
		}
		if len(value) != 0 {
			values = append(values, string(value))
		}
	}
	current, err := resolver.Environments.Get(ctx, pin.EnvironmentID)
	if err != nil || current == nil || current.ID != env.ID || current.Version != pin.EnvironmentVersion || model.IsRegistryCredentialEnvironment(current) {
		return nil, ErrRuntimeLogUnavailable
	}
	return values, nil
}

func (r *RuntimeService) admitNativeStageLogs(ctx context.Context, s NativeRuntimeSubject, jobs jobqueue.Store, resolver *AppEnvResolver) (*nativeRuntimeBinding, []string, error) {
	b, err := r.nativeStageLogBinding(ctx, s, jobs)
	if err != nil {
		return nil, nil, err
	}
	values, err := nativeStageLogSecrets(ctx, b, resolver)
	if err != nil {
		return nil, nil, err
	}
	// Secret resolution can outlive the authority read. Re-read the accepted
	// binding before inspecting or exposing bytes rather than using its old copy.
	confirmed, err := r.nativeStageLogBinding(ctx, s, jobs)
	if err != nil || !reflect.DeepEqual(nativeLogMetadata(b), nativeLogMetadata(confirmed)) {
		return nil, nil, ErrRuntimeLogUnavailable
	}
	logCtx := WithLogSecrets(ctx, values)
	if observed := r.inspectNativeStage(logCtx, confirmed); observed.Binding != "verified" {
		return nil, nil, ErrRuntimeLogUnavailable
	}
	return confirmed, values, nil
}

func (r *RuntimeService) NativeStageLogReplicas(ctx context.Context, s NativeRuntimeSubject, jobs jobqueue.Store) (NativeRuntimeLogReplicas, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	b, err := r.nativeStageLogBinding(ctx, s, jobs)
	if err != nil {
		return NativeRuntimeLogReplicas{}, err
	}
	observed := r.inspectNativeStage(ctx, b)
	if observed.Binding != "verified" {
		return NativeRuntimeLogReplicas{}, ErrRuntimeLogUnavailable
	}
	return NativeRuntimeLogReplicas{Subject: b.subject, RunID: b.subject.RunID, Replicas: []RuntimeLogReplica{{ContainerID: b.receipt.ContainerID, Name: b.receipt.Name, State: observed.State}}}, nil
}

func (r *RuntimeService) OpenNativeStageLogSession(ctx context.Context, s NativeRuntimeSubject, jobs jobqueue.Store, resolver *AppEnvResolver, options RuntimeLogOptions) (NativeRuntimeLogPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s, err := nativeLogSubject(s)
	if err != nil {
		return NativeRuntimeLogPage{}, err
	}
	if r == nil {
		return NativeRuntimeLogPage{}, ErrRuntimeLogUnavailable
	}
	c := r.runtimeLogCache()
	opts, err := normalizeRuntimeLogOptions(options, c.now())
	if err != nil {
		return NativeRuntimeLogPage{}, err
	}
	if opts.ExpectedRunID != "" && opts.ExpectedRunID != s.RunID {
		return NativeRuntimeLogPage{}, ErrRuntimeLogRequest
	}
	if err = c.reserveCapture(); err != nil {
		return NativeRuntimeLogPage{}, err
	}
	defer c.releaseCapture()
	binding, values, err := r.admitNativeStageLogs(ctx, s, jobs, resolver)
	if err != nil {
		return NativeRuntimeLogPage{}, err
	}
	if opts.ContainerID != "" && opts.ContainerID != binding.receipt.ContainerID {
		return NativeRuntimeLogPage{}, ErrRuntimeLogRequest
	}
	metadata := nativeLogMetadata(binding)
	b := &boundedRuntimeLogBuffer{limit: runtimeLogByteLimit}
	writer := NewSecretLogWriter(WithLogSecrets(ctx, values), b)
	cmd := exec.CommandContext(ctx, r.dockerBin(), "logs", "--timestamps", "--tail", strconv.Itoa(opts.MaxLines), "--since", opts.Since.Format(time.RFC3339Nano), "--until", opts.Until.Format(time.RFC3339Nano), binding.receipt.ContainerID)
	cmd.Stdout, cmd.Stderr = writer, writer
	cmd.WaitDelay = 250 * time.Millisecond
	runErr, closeErr := cmd.Run(), writer.Close()
	if runErr != nil || closeErr != nil {
		return NativeRuntimeLogPage{}, ErrRuntimeLogUnavailable
	}
	// A changed identity/generation during capture cannot publish the first
	// page. No unredacted bytes or resolved secrets enter the session cache.
	after, _, err := r.admitNativeStageLogs(ctx, s, jobs, resolver)
	if err != nil || !reflect.DeepEqual(metadata, nativeLogMetadata(after)) {
		return NativeRuntimeLogPage{}, ErrRuntimeLogUnavailable
	}
	entries, tailReached, err := parseRuntimeLogEntries(b, opts)
	if err != nil {
		return NativeRuntimeLogPage{}, err
	}
	serialized, err := json.Marshal(metadata)
	if err != nil {
		return NativeRuntimeLogPage{}, ErrRuntimeLogUnavailable
	}
	snapshot := &runtimeLogSnapshot{subject: nativeRuntimeLogSubject(s), native: metadata,
		page:    RuntimeLogPage{ContainerID: binding.receipt.ContainerID, Since: opts.Since, Until: opts.Until, TailLimitReached: tailReached, Coverage: "Bounded Docker tail in capture order; older or driver-expired logs may be unavailable."},
		entries: entries, bytes: b.Len() + len(entries)*128 + 1024 + len(serialized)}
	if err = c.save(snapshot); err != nil {
		return NativeRuntimeLogPage{}, err
	}
	return nativeRuntimeLogPage(snapshot, 0, 200), nil
}

func nativeRuntimeLogPage(s *runtimeLogSnapshot, offset, limit int) NativeRuntimeLogPage {
	p := runtimeLogPage(s, offset, limit)
	return NativeRuntimeLogPage{Subject: s.native.Subject, RunID: s.native.Subject.RunID, SessionID: p.SessionID, ContainerID: p.ContainerID, Since: p.Since, Until: p.Until, ExpiresAt: p.ExpiresAt,
		Offset: p.Offset, NextOffset: p.NextOffset, Total: p.Total, TailLimitReached: p.TailLimitReached, Coverage: p.Coverage, Entries: p.Entries}
}

func (r *RuntimeService) retainedNativeStageLogSession(ctx context.Context, s NativeRuntimeSubject, id string, jobs jobqueue.Store, resolver *AppEnvResolver) (*runtimeLogSnapshot, error) {
	s, err := nativeLogSubject(s)
	if err != nil {
		return nil, err
	}
	if !nativeRuntimeID(id) {
		return nil, ErrRuntimeLogRequest
	}
	if r == nil {
		return nil, ErrRuntimeLogUnavailable
	}
	snapshot, err := r.runtimeLogCache().load(nativeRuntimeLogSubject(s), id)
	if err != nil {
		return nil, err
	}
	binding, _, err := r.admitNativeStageLogs(ctx, s, jobs, resolver)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(snapshot.native, nativeLogMetadata(binding)) {
		return nil, ErrRuntimeLogUnavailable
	}
	return snapshot, nil
}

func (r *RuntimeService) ReadNativeStageLogSession(ctx context.Context, s NativeRuntimeSubject, id string, jobs jobqueue.Store, resolver *AppEnvResolver, offset, limit int) (NativeRuntimeLogPage, error) {
	if offset < 0 || limit < 1 || limit > 500 {
		return NativeRuntimeLogPage{}, ErrRuntimeLogRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	snapshot, err := r.retainedNativeStageLogSession(ctx, s, id, jobs, resolver)
	if err != nil {
		return NativeRuntimeLogPage{}, err
	}
	if offset > len(snapshot.entries) {
		return NativeRuntimeLogPage{}, ErrRuntimeLogRequest
	}
	return nativeRuntimeLogPage(snapshot, offset, limit), nil
}

func (r *RuntimeService) DownloadNativeStageLogSession(ctx context.Context, s NativeRuntimeSubject, id string, jobs jobqueue.Store, resolver *AppEnvResolver) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	snapshot, err := r.retainedNativeStageLogSession(ctx, s, id, jobs, resolver)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, entry := range snapshot.entries {
		if entry.Timestamp != "" {
			fmt.Fprint(&b, entry.Timestamp, " ")
		}
		fmt.Fprintln(&b, entry.Message)
	}
	return b.String(), nil
}

// Freeing an invalidated session requires current pipeline/run ownership, not
// a still-valid receipt, accepted job, credential generation or container.
func (r *RuntimeService) CloseNativeStageLogSession(ctx context.Context, s NativeRuntimeSubject, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s, err := nativeLogSubject(s)
	if err != nil || !nativeRuntimeID(id) {
		return ErrRuntimeLogRequest
	}
	if r == nil || r.Store == nil || r.Store.Pipelines == nil || r.Store.Runs == nil {
		return ErrRuntimeLogUnavailable
	}
	p, err := r.Store.Pipelines.Get(ctx, s.PipelineID)
	if errors.Is(err, store.ErrNotFound) || err == nil && (p == nil || p.ID != s.PipelineID) {
		return ErrRuntimeLogExpired
	}
	if err != nil {
		return ErrRuntimeLogUnavailable
	}
	run, err := r.Store.Runs.GetSummary(ctx, s.RunID)
	if errors.Is(err, store.ErrNotFound) || err == nil && (run == nil || run.ID != s.RunID || run.PipelineID != s.PipelineID) {
		return ErrRuntimeLogExpired
	}
	if err != nil {
		return ErrRuntimeLogUnavailable
	}
	r.runtimeLogCache().close(nativeRuntimeLogSubject(s), id)
	return nil
}

func nativeRuntimeLogChannel(s NativeRuntimeSubject, id string) string {
	// The encoded tuple is injective for printable colon/Unicode/percent IDs;
	// joining unconstrained opaque IDs with delimiters would not be.
	b, _ := json.Marshal([]string{"native-stage", s.PipelineID, s.RunID, s.StageID, id})
	return "native-runtime-logs:" + base64.RawURLEncoding.EncodeToString(b)
}

func (r *RuntimeService) PrepareNativeStageLogs(ctx context.Context, s NativeRuntimeSubject, jobs jobqueue.Store, resolver *AppEnvResolver) (string, func(context.Context, io.Writer) error, error) {
	admission, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	binding, values, err := r.admitNativeStageLogs(admission, s, jobs, resolver)
	if err != nil {
		return "", nil, err
	}
	metadata := nativeLogMetadata(binding)
	channel := nativeRuntimeLogChannel(binding.subject, binding.receipt.ContainerID)
	// Only this private closure retains the selected values. It does not keep
	// the admission context/deadline or place values in service/cache state.
	produce := func(connection context.Context, out io.Writer) error {
		check, cancelCheck := context.WithTimeout(connection, 15*time.Second)
		current, _, err := r.admitNativeStageLogs(check, metadata.Subject, jobs, resolver)
		cancelCheck()
		if err != nil || !reflect.DeepEqual(metadata, nativeLogMetadata(current)) {
			return ErrRuntimeLogUnavailable
		}
		logCtx := WithLogSecrets(connection, values)
		writer := NewSecretLogWriter(logCtx, out)
		cmd := exec.CommandContext(logCtx, r.dockerBin(), "logs", "--follow", "--tail", "200", metadata.Receipt.ContainerID)
		cmd.Stdout, cmd.Stderr = writer, writer
		cmd.WaitDelay = 250 * time.Millisecond
		runErr, closeErr := cmd.Run(), writer.Close()
		if runErr != nil || closeErr != nil {
			return ErrRuntimeLogUnavailable
		}
		return nil
	}
	return channel, produce, nil
}
