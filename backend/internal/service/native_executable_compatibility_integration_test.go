//go:build integration

package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/pusher"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
	storepg "github.com/santapong/cooker/internal/store/postgres"
)

// Deliberately portable to the exact retained727-source runner/executor. No
// current source/registry types or production compatibility patch are imported.
type executableCompatCase struct {
	Name, JobID, RunID, PipelineID, IntentID, PayloadDigest string
	PipelineVersion                                         int
	SourceBound                                             bool
	Payload                                                 json.RawMessage
}
type executableCompatGroup struct {
	Schema string
	Cases  []executableCompatCase
}
type executableCompatSeed struct {
	SchemaVersion int
	Groups        map[string]executableCompatGroup
}
type executableCompatProbe struct {
	build, push, deploy, registry atomic.Int32
	context, dockerfile           string
}

func (p *executableCompatProbe) Build(_ context.Context, req builder.Request) (builder.Result, error) {
	p.build.Add(1)
	p.context, p.dockerfile = req.ContextDir, req.Dockerfile
	return builder.Result{}, errors.New("owned adapter boundary denied build")
}
func (p *executableCompatProbe) Push(_ context.Context, _ pusher.Request) (pusher.Result, error) {
	p.push.Add(1)
	return pusher.Result{}, errors.New("owned adapter boundary denied push")
}
func (p *executableCompatProbe) Deploy(_ context.Context, _ deployer.Request) (deployer.Result, error) {
	p.deploy.Add(1)
	return deployer.Result{}, errors.New("owned adapter boundary denied deploy")
}

func TestNativeExecutableCompatibilityCurrentPayload(t *testing.T) {
	dsn, path, consumer := os.Getenv("COOKER_NATIVE_EXECUTABLE_TEST_DATABASE_URL"), os.Getenv("COOKER_NATIVE_EXECUTABLE_SEED_FILE"), os.Getenv("COOKER_NATIVE_EXECUTABLE_CONSUMER")
	if dsn == "" || path == "" || consumer == "" {
		t.Skip("owned emitted-payload executable fixture required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "cooker-native-executable-pg" || u.Port() != "5432" || u.Path != "/cooker_native_executable_test" || u.User == nil || u.User.Username() != "native_executable_fixture" || u.Query().Get("sslmode") != "disable" || len(u.Query()) != 1 || u.Fragment != "" {
		t.Fatal("refusing non-owned executable fixture")
	}
	if _, password := u.User.Password(); password {
		t.Fatal("fixture must not use credentials")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture executableCompatSeed
	if json.Unmarshal(data, &fixture) != nil || fixture.SchemaVersion != 1 {
		t.Fatal("emitted fixture unavailable")
	}
	group, ok := fixture.Groups[consumer]
	if !ok || !strings.HasPrefix(group.Schema, "native_executable_") || len(group.Schema) != len("native_executable_")+32 || strings.Trim(group.Schema[len("native_executable_"):], "0123456789abcdef") != "" {
		t.Fatal("fixture schema identity invalid")
	}
	q := u.Query()
	q.Set("search_path", group.Schema)
	u.RawQuery = q.Encode()
	ctx := context.Background()
	st, err := storepg.NewStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	jobs := jobqueue.NewPostgres(db)
	jobs.OnTransition = storepg.SyncJobRun
	reports := []map[string]any{}
	for _, expected := range group.Cases {
		t.Run(expected.Name, func(t *testing.T) {
			j, err := jobs.Dequeue(ctx, "owned-"+consumer+"-worker", []string{JobKindPipelineRun})
			if err != nil || j == nil || j.ID != expected.JobID || j.Attempts != 1 || j.Status != jobqueue.StatusRunning || j.LockedBy == "" {
				t.Fatal("actual claim does not match emitted receipt", err)
			}
			digest, err := jobqueue.PayloadDigest(j.Payload)
			originalDigest, rawErr := jobqueue.PayloadDigest(expected.Payload)
			if err != nil || rawErr != nil || digest != expected.PayloadDigest || originalDigest != digest {
				t.Fatal("accepted JSON was altered between producer and older consumer")
			}
			var decoded runJobPayload
			if json.Unmarshal(j.Payload, &decoded) != nil || decoded.Run == nil || decoded.Pipeline == nil || decoded.Run.ID != expected.RunID || decoded.Pipeline.ID != expected.PipelineID || decoded.Run.PipelineVersion != expected.PipelineVersion || decoded.Run.Status != model.RunStatusPending {
				t.Fatal("exact emitted identities/version/actor unavailable")
			}
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(j.Payload, &raw)
			encodedTyped, _ := json.Marshal(decoded)
			var typed map[string]json.RawMessage
			_ = json.Unmarshal(encodedTyped, &typed)
			probe := &executableCompatProbe{}
			executor := NewExecutor(WithBuilder(probe), WithPusher(probe), WithDeployer(probe), WithDockerDeployer(probe), WithRunUpdater(func(c context.Context, r *model.PipelineRun) error {
				return st.Runs.UpdateProgress(c, r.ID, r.StageRuns)
			}))
			executor.registryAuth = func(context.Context, string) (authn.Authenticator, error) {
				probe.registry.Add(1)
				return nil, errors.New("owned registry resolution denied")
			}
			runner := NewJobQueueRunner(st, executor, nil)
			executableCompatConfigureRunner(runner, executor, st, db)
			claim := store.WithRunClaim(ctx, j.ID, j.LockedBy, nil)
			handleErr := runner.Handle(claim, j)
			// Failure adapters cannot perform effects; retain the queue uncertainty
			// honestly rather than falsely complete a fixture publication.
			if handleErr != nil {
				if err = jobs.Suspend(ctx, j.LockedBy, j.ID, "owned executable compatibility probe completed"); err != nil {
					t.Fatal(err)
				}
			} else if err = jobs.Complete(ctx, j.LockedBy, j.ID); err != nil {
				t.Fatal(err)
			}
			final, err := jobs.Get(ctx, j.ID)
			finalDigest, digestErr := jobqueue.PayloadDigest(final.Payload)
			if err != nil || digestErr != nil || finalDigest != digest {
				t.Fatal("consumer rewrote immutable accepted payload")
			}
			report := map[string]any{"name": expected.Name, "consumer": consumer, "jobId": j.ID, "runId": expected.RunID, "pipelineId": expected.PipelineID, "pipelineVersion": expected.PipelineVersion, "payloadDigest": digest, "claimAttempts": j.Attempts, "claimOwner": j.LockedBy, "sourceBound": expected.SourceBound, "wireHasSource": len(raw["source"]) > 0, "typedRetainsSource": len(typed["source"]) > 0, "wireHasTrigger": len(raw["trigger"]) > 0, "typedRetainsTrigger": len(typed["trigger"]) > 0, "wireHasRegistry": len(raw["registryAuthority"]) > 0, "typedRetainsRegistry": len(typed["registryAuthority"]) > 0, "buildCalls": probe.build.Load(), "pushCalls": probe.push.Load(), "deployCalls": probe.deploy.Load(), "registryCalls": probe.registry.Load(), "buildContext": probe.context, "dockerfile": probe.dockerfile, "handlerReturnedError": handleErr != nil, "handlerOutcomeUnknown": errors.Is(handleErr, jobqueue.ErrOutcomeUnknown), "finalQueueStatus": final.Status, "actorSubject": decoded.Run.StartedByUserSub, "actorEmail": decoded.Run.StartedByEmail, "actorGroups": decoded.Run.StartedByGroups}
			reports = append(reports, report)
			if expected.SourceBound {
				if probe.build.Load()+probe.push.Load()+probe.deploy.Load()+probe.registry.Load() != 0 {
					t.Error("source-bound current job reached application adapter in incompatible executable")
				}
			} else {
				want := map[string]int32{"regular-build": probe.build.Load(), "regular-push": probe.push.Load(), "regular-deploy": probe.deploy.Load()}[expected.Name]
				if want != 1 || probe.build.Load()+probe.push.Load()+probe.deploy.Load() != 1 {
					t.Error("regular native positive instrumentation control did not reach exactly its selected adapter")
				}
			}
		})
	}
	if output := os.Getenv("COOKER_NATIVE_EXECUTABLE_RESULT_FILE"); output != "" {
		data, err := json.MarshalIndent(map[string]any{"scope": "owned-executable-current-native-payload-probe", "consumer": consumer, "cases": reports, "realApplicationEffects": false, "actualSQLClaims": true, "sourceHookAvailableInOldExecutable": false}, "", "  ")
		if err != nil || os.WriteFile(output, append(data, '\n'), 0600) != nil {
			t.Fatal("safe executable report unavailable")
		}
	}
}
