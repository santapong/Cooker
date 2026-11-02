package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
)

var (
	ErrRuntimeLogRequest     = errors.New("invalid runtime log request")
	ErrRuntimeLogUnavailable = errors.New("runtime log session unavailable")
	ErrRuntimeLogExpired     = errors.New("runtime log session expired or not found; open a new session")
	ErrRuntimeLogCapacity    = errors.New("runtime log session capacity reached; close a session or retry after expiry")
)

const runtimeLogByteLimit = 1024 * 1024

// RuntimeLogOptions selects one immutable container and a bounded historical
// window. Search is literal and applies only after known secrets are redacted.
type RuntimeLogOptions struct {
	ContainerID   string    `json:"containerId,omitempty"`
	ExpectedRunID string    `json:"expectedRunId,omitempty"`
	Since         time.Time `json:"since,omitempty"`
	Until         time.Time `json:"until,omitempty"`
	Search        string    `json:"search,omitempty"`
	MaxLines      int       `json:"maxLines,omitempty"`
}

type RuntimeLogReplica struct {
	ContainerID string `json:"containerId"`
	Name        string `json:"name"`
	State       string `json:"state"`
}

type RuntimeLogReplicas struct {
	AppID    string              `json:"appId"`
	Service  string              `json:"service"`
	RunID    string              `json:"runId"`
	Replicas []RuntimeLogReplica `json:"replicas"`
}

type RuntimeLogEntry struct {
	Sequence  int    `json:"sequence"`
	Timestamp string `json:"timestamp,omitempty"`
	Message   string `json:"message"`
}

// Pages retain capture order, not an invented cross-stream causal order.
// Sessions live for ten minutes in bounded process memory; restart/expiry is
// explicit, and a reconnect never silently moves to a replacement container.
type RuntimeLogPage struct {
	SessionID        string            `json:"sessionId"`
	AppID            string            `json:"appId"`
	Service          string            `json:"service"`
	RunID            string            `json:"runId"`
	ContainerID      string            `json:"containerId"`
	Since            time.Time         `json:"since"`
	Until            time.Time         `json:"until"`
	ExpiresAt        time.Time         `json:"expiresAt"`
	Offset           int               `json:"offset"`
	NextOffset       *int              `json:"nextOffset,omitempty"`
	Total            int               `json:"total"`
	TailLimitReached bool              `json:"tailLimitReached"`
	Coverage         string            `json:"coverage"`
	Entries          []RuntimeLogEntry `json:"entries"`
}

type runtimeLogSnapshot struct {
	subject    runtimeLogSubject
	page       RuntimeLogPage
	deployment model.AppDeploy
	native     *nativeRuntimeLogMetadata
	entries    []RuntimeLogEntry
	bytes      int
}

// A comparable tagged identity keeps the existing App and native sessions in
// one budget without allowing equal IDs in different namespaces to collide.
type runtimeLogSubject struct {
	kind, appID, service, pipelineID, runID, stageID string
}

func appRuntimeLogSubject(appID, service string) runtimeLogSubject {
	return runtimeLogSubject{kind: "app-service", appID: appID, service: service}
}

func nativeRuntimeLogSubject(s NativeRuntimeSubject) runtimeLogSubject {
	return runtimeLogSubject{kind: "native-stage", pipelineID: s.PipelineID, runID: s.RunID, stageID: s.StageID}
}

func (s runtimeLogSubject) valid() bool {
	switch s.kind {
	case "app-service":
		// Keep the existing App wrapper's exact string matching behavior.
		return s.pipelineID == "" && s.runID == "" && s.stageID == ""
	case "native-stage":
		return s.appID == "" && s.service == "" && nativeRuntimeID(s.pipelineID) && nativeRuntimeID(s.runID) && nativeRuntimeID(s.stageID)
	default:
		return false
	}
}

type runtimeLogSessionCache struct {
	mu       sync.Mutex
	sessions map[string]*runtimeLogSnapshot
	bytes    int
	maxBytes int
	maxCount int
	ttl      time.Duration
	now      func() time.Time
	captures int
}

func (r *RuntimeService) runtimeLogCache() *runtimeLogSessionCache {
	r.logSessionsMu.Lock()
	defer r.logSessionsMu.Unlock()
	if r.logSessions == nil {
		r.logSessions = &runtimeLogSessionCache{sessions: map[string]*runtimeLogSnapshot{}, maxBytes: 16 * runtimeLogByteLimit, maxCount: 128, ttl: 10 * time.Minute, now: time.Now}
	}
	return r.logSessions
}

func (c *runtimeLogSessionCache) purge() {
	for id, s := range c.sessions {
		if !c.now().Before(s.page.ExpiresAt) {
			c.bytes -= s.bytes
			delete(c.sessions, id)
		}
	}
}

func (c *runtimeLogSessionCache) save(s *runtimeLogSnapshot) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purge()
	if s == nil || !s.subject.valid() || (s.subject.kind == "native-stage") != (s.native != nil) {
		return ErrRuntimeLogRequest
	}
	if c.bytes+s.bytes > c.maxBytes || len(c.sessions) >= c.maxCount {
		return ErrRuntimeLogCapacity
	}
	s.page.SessionID = uuid.NewString()
	s.page.ExpiresAt = c.now().Add(c.ttl).UTC()
	c.sessions[s.page.SessionID] = s
	c.bytes += s.bytes
	return nil
}

func (c *runtimeLogSessionCache) reserveCapture() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purge()
	if c.captures >= 4 || len(c.sessions) >= c.maxCount || c.bytes >= c.maxBytes {
		return ErrRuntimeLogCapacity
	}
	c.captures++
	return nil
}

func (c *runtimeLogSessionCache) releaseCapture() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.captures--
}

func (c *runtimeLogSessionCache) load(subject runtimeLogSubject, id string) (*runtimeLogSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purge()
	s := c.sessions[id]
	if !subject.valid() || s == nil || s.subject != subject {
		return nil, ErrRuntimeLogExpired
	}
	return s, nil // snapshots are immutable after insertion
}

func (c *runtimeLogSessionCache) close(subject runtimeLogSubject, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purge()
	if s := c.sessions[id]; subject.valid() && s != nil && s.subject == subject {
		c.bytes -= s.bytes
		delete(c.sessions, id)
	}
}

func (r *RuntimeService) CloseRuntimeLogSession(appID, service, id string) {
	r.runtimeLogCache().close(appRuntimeLogSubject(appID, service), id)
}

func normalizeRuntimeLogOptions(o RuntimeLogOptions, now time.Time) (RuntimeLogOptions, error) {
	if len(o.Search) > 256 || len(o.ExpectedRunID) > 128 || (o.ContainerID != "" && !runtimeContainerID.MatchString(o.ContainerID)) {
		return o, ErrRuntimeLogRequest
	}
	if o.MaxLines == 0 {
		o.MaxLines = 2000
	}
	if o.MaxLines < 1 || o.MaxLines > 10000 {
		return o, ErrRuntimeLogRequest
	}
	if o.Until.IsZero() {
		o.Until = now.UTC()
	}
	if o.Since.IsZero() {
		o.Since = o.Until.Add(-time.Hour)
	}
	if !o.Since.Before(o.Until) || o.Until.Sub(o.Since) > 24*time.Hour || o.Until.After(now.Add(time.Minute)) {
		return o, ErrRuntimeLogRequest
	}
	o.Since, o.Until = o.Since.UTC(), o.Until.UTC()
	return o, nil
}

type boundedRuntimeLogBuffer struct {
	data  bytes.Buffer
	limit int
}

func (b *boundedRuntimeLogBuffer) Len() int       { return b.data.Len() }
func (b *boundedRuntimeLogBuffer) Bytes() []byte  { return b.data.Bytes() }
func (b *boundedRuntimeLogBuffer) String() string { return b.data.String() }

func (b *boundedRuntimeLogBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, ErrRuntimeLogRequest
	}
	return b.data.Write(p)
}

func (r *RuntimeService) logCLIOutput(ctx context.Context, args ...string) ([]byte, error) {
	b := &boundedRuntimeLogBuffer{limit: 64 * 1024}
	cmd := exec.CommandContext(ctx, r.dockerBin(), args...)
	cmd.Stdout, cmd.Stderr = b, io.Discard
	if cmd.Run() != nil {
		return nil, ErrRuntimeLogUnavailable
	}
	return b.Bytes(), nil
}

// captureLogReplicas derives ownership from the recorded runtime, not the edited
// App or caller-supplied labels. Compose replicas additionally require exact
// project/service labels, a generated replica name, and the trusted run label.
func (r *RuntimeService) captureLogReplicas(ctx context.Context, app *model.App, service string, o *runtimeObservation) (RuntimeLogReplicas, error) {
	result := RuntimeLogReplicas{AppID: app.ID, Service: service, RunID: o.deployment.RunID, Replicas: []RuntimeLogReplica{}}
	bound, name, err := o.workload(app, service)
	if err != nil || bound.DeployTarget.Kind != model.DeployTargetDockerHost || result.RunID == "" || o.deployment.Status == model.RunStatusNeedsAttention {
		return result, ErrRuntimeLogUnavailable
	}
	compose := false
	for _, stage := range o.stages {
		if runtimeServiceKey(stage) == service {
			compose = stage.Config.DeployRuntime == "compose"
		}
	}
	targets := []string{name}
	project := ""
	if compose {
		suffix := "-" + service + "-1"
		if !strings.HasSuffix(name, suffix) {
			return result, ErrRuntimeLogUnavailable
		}
		project = strings.TrimSuffix(name, suffix)
		if project == "" {
			return result, ErrRuntimeLogUnavailable
		}
		data, err := r.logCLIOutput(ctx, "ps", "--all", "--no-trunc", "--filter", "label="+RuntimeRunLabel+"="+result.RunID, "--filter", "label=com.docker.compose.project="+project, "--filter", "label=com.docker.compose.service="+service, "--format", "{{.ID}}")
		if err != nil {
			return result, err
		}
		targets = strings.Fields(string(data))
	}
	if len(targets) == 0 || len(targets) > 128 {
		return result, ErrRuntimeLogUnavailable
	}
	format := `{{.Id}}|{{index .Config.Labels "io.cooker.run-id"}}|{{.Name}}|{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.oneoff"}}|{{.State.Status}}`
	seen := map[string]bool{}
	for _, target := range targets {
		if compose && !runtimeContainerID.MatchString(target) {
			return result, ErrRuntimeLogUnavailable
		}
		data, err := r.logCLIOutput(ctx, "inspect", "-f", format, target)
		if err != nil {
			return result, err
		}
		p := strings.Split(strings.TrimSpace(string(data)), "|")
		if len(p) != 7 || !runtimeContainerID.MatchString(p[0]) || p[1] != result.RunID || seen[p[0]] {
			return result, ErrRuntimeLogUnavailable
		}
		containerName := strings.TrimPrefix(p[2], "/")
		if compose {
			prefix := project + "-" + service + "-"
			if p[3] != project || p[4] != service || strings.EqualFold(p[5], "true") || !strings.HasPrefix(containerName, prefix) {
				return result, ErrRuntimeLogUnavailable
			}
			replica, err := strconv.Atoi(strings.TrimPrefix(containerName, prefix))
			if err != nil || replica < 1 || replica > 10000 {
				return result, ErrRuntimeLogUnavailable
			}
		} else if containerName != name {
			return result, ErrRuntimeLogUnavailable
		}
		seen[p[0]] = true
		result.Replicas = append(result.Replicas, RuntimeLogReplica{ContainerID: p[0], Name: containerName, State: p[6]})
	}
	sort.Slice(result.Replicas, func(i, j int) bool { return result.Replicas[i].Name < result.Replicas[j].Name })
	return result, nil
}

func (r *RuntimeService) DeploymentLogReplicas(ctx context.Context, app *model.App, service string) (RuntimeLogReplicas, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	o, err := r.observe(ctx, app)
	if err != nil {
		return RuntimeLogReplicas{}, ErrRuntimeLogUnavailable
	}
	return r.captureLogReplicas(ctx, app, service, o)
}

func (r *RuntimeService) OpenRuntimeLogSession(ctx context.Context, app *model.App, service string, jobs jobqueue.Store, resolver *AppEnvResolver, options RuntimeLogOptions) (RuntimeLogPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c := r.runtimeLogCache()
	opts, err := normalizeRuntimeLogOptions(options, c.now())
	if err != nil {
		return RuntimeLogPage{}, err
	}
	if err := c.reserveCapture(); err != nil {
		return RuntimeLogPage{}, err
	}
	defer c.releaseCapture()
	o, err := r.observe(ctx, app)
	if err != nil || (opts.ExpectedRunID != "" && opts.ExpectedRunID != o.deployment.RunID) || resolver == nil {
		return RuntimeLogPage{}, ErrRuntimeLogUnavailable
	}
	replicas, err := r.captureLogReplicas(ctx, app, service, o)
	if err != nil {
		return RuntimeLogPage{}, err
	}
	id := opts.ContainerID
	if id == "" && len(replicas.Replicas) == 1 {
		id = replicas.Replicas[0].ContainerID
	}
	found := false
	for _, replica := range replicas.Replicas {
		found = found || replica.ContainerID == id
	}
	if !found {
		return RuntimeLogPage{}, ErrRuntimeLogRequest
	}
	resolved, err := deploymentLogEnvironment(ctx, app.ID, &o.deployment, jobs, resolver, !r.SecretGenerationBound)
	if err != nil {
		return RuntimeLogPage{}, ErrRuntimeLogUnavailable
	}
	logCtx := WithLogSecrets(ctx, resolved.SecretValues)
	b := &boundedRuntimeLogBuffer{limit: runtimeLogByteLimit}
	w := NewSecretLogWriter(logCtx, b)
	cmd := exec.CommandContext(ctx, r.dockerBin(), "logs", "--timestamps", "--tail", strconv.Itoa(opts.MaxLines), "--since", opts.Since.Format(time.RFC3339Nano), "--until", opts.Until.Format(time.RFC3339Nano), id)
	cmd.Stdout, cmd.Stderr = w, w
	runErr := cmd.Run()
	closeErr := w.Close()
	if runErr != nil || closeErr != nil {
		return RuntimeLogPage{}, ErrRuntimeLogUnavailable
	}
	// Rotation during capture must also refuse the first page, not only later
	// pages. No resolved credentials or this context enter the session cache.
	if _, err := deploymentLogEnvironment(ctx, app.ID, &o.deployment, jobs, resolver, !r.SecretGenerationBound); err != nil {
		return RuntimeLogPage{}, ErrRuntimeLogUnavailable
	}
	entries, tailReached, err := parseRuntimeLogEntries(b, opts)
	if err != nil {
		return RuntimeLogPage{}, err
	}
	s := &runtimeLogSnapshot{subject: appRuntimeLogSubject(app.ID, service), page: RuntimeLogPage{AppID: app.ID, Service: service, RunID: o.deployment.RunID, ContainerID: id, Since: opts.Since, Until: opts.Until, TailLimitReached: tailReached, Coverage: "Bounded Docker tail in capture order; older or driver-expired logs may be unavailable."}, deployment: o.deployment, entries: entries, bytes: b.Len() + len(entries)*128 + 1024}
	if err := c.save(s); err != nil {
		return RuntimeLogPage{}, err
	}
	return runtimeLogPage(s, 0, 200), nil
}

// Parsing and search consume only bytes already protected by SecretLogWriter.
func parseRuntimeLogEntries(b *boundedRuntimeLogBuffer, opts RuntimeLogOptions) ([]RuntimeLogEntry, bool, error) {
	entries := []RuntimeLogEntry{}
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	if b.Len() == 0 {
		lines = nil
	}
	if len(lines) > 10000 {
		return nil, false, ErrRuntimeLogUnavailable
	}
	for _, line := range lines {
		if opts.Search != "" && !strings.Contains(line, opts.Search) {
			continue
		}
		entry := RuntimeLogEntry{Sequence: len(entries) + 1, Message: line}
		if stamp, message, ok := strings.Cut(line, " "); ok {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
				entry.Timestamp, entry.Message = stamp, message
			}
		}
		entries = append(entries, entry)
	}
	return entries, len(lines) >= opts.MaxLines, nil
}

func runtimeLogPage(s *runtimeLogSnapshot, offset, limit int) RuntimeLogPage {
	p := s.page
	p.Offset, p.Total = offset, len(s.entries)
	end := min(len(s.entries), offset+limit)
	p.Entries = append([]RuntimeLogEntry{}, s.entries[offset:end]...)
	if end < len(s.entries) {
		next := end
		p.NextOffset = &next
	}
	return p
}

func (r *RuntimeService) retainedRuntimeLogSession(ctx context.Context, appID, service, id string, jobs jobqueue.Store, resolver *AppEnvResolver) (*runtimeLogSnapshot, error) {
	s, err := r.runtimeLogCache().load(appRuntimeLogSubject(appID, service), id)
	if err != nil {
		return nil, err
	}
	if resolver == nil {
		return nil, ErrRuntimeLogUnavailable
	}
	// Revalidate accepted configuration on every page/download. Rotation or
	// lost historical generations may not expose cached historical log bytes.
	if _, err := deploymentLogEnvironment(ctx, appID, &s.deployment, jobs, resolver, !r.SecretGenerationBound); err != nil {
		return nil, ErrRuntimeLogUnavailable
	}
	return s, nil
}

func (r *RuntimeService) ReadRuntimeLogSession(ctx context.Context, appID, service, id string, jobs jobqueue.Store, resolver *AppEnvResolver, offset, limit int) (RuntimeLogPage, error) {
	if offset < 0 || limit < 1 || limit > 500 {
		return RuntimeLogPage{}, ErrRuntimeLogRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s, err := r.retainedRuntimeLogSession(ctx, appID, service, id, jobs, resolver)
	if err != nil {
		return RuntimeLogPage{}, err
	}
	if offset > len(s.entries) {
		return RuntimeLogPage{}, ErrRuntimeLogRequest
	}
	return runtimeLogPage(s, offset, limit), nil
}

func (r *RuntimeService) DownloadRuntimeLogSession(ctx context.Context, appID, service, id string, jobs jobqueue.Store, resolver *AppEnvResolver) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s, err := r.retainedRuntimeLogSession(ctx, appID, service, id, jobs, resolver)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, entry := range s.entries {
		if entry.Timestamp != "" {
			fmt.Fprint(&b, entry.Timestamp, " ")
		}
		fmt.Fprintln(&b, entry.Message)
	}
	return b.String(), nil
}
