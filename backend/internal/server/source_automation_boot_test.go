package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/santapong/cooker/internal/governance"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store/memory"
)

type bootSourceFixture struct{ reads int }

func (f *bootSourceFixture) Identity(context.Context, *connection.Connection, string) (model.GitSourceIdentity, error) {
	f.reads++
	return model.GitSourceIdentity{}, io.EOF
}
func (f *bootSourceFixture) Revision(context.Context, *connection.Connection, string, string) (service.SourceRevision, error) {
	f.reads++
	return service.SourceRevision{}, io.EOF
}
func (f *bootSourceFixture) CheckoutSource(context.Context, *model.PipelineSourcePlan, io.Writer) (string, error) {
	f.reads++
	return "", io.EOF
}
func bootNativePipeline() *model.Pipeline {
	return &model.Pipeline{ID: "native", Name: "Native Shop", Source: &model.PipelineSourceConfig{ConnectionID: "source", Repository: "owned/repository", FullRef: "refs/heads/main"}, Stages: []model.Stage{{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Dockerfile: "Dockerfile"}}, {ID: "deploy", Type: model.StageTypeDeploy, EnvironmentID: "env-b", Config: model.StageConfig{DeployRuntime: "docker"}}}}
}

func TestNativeSourceBootUsesOneTrustedPreparerAndDurableQueue(t *testing.T) {
	st := memory.New()
	jobs := jobqueue.NewMemory()
	source := &bootSourceFixture{}
	triggers := &service.TriggerService{Store: memory.NewTriggerStore()}
	deps := &jobQueueDeps{Store: jobs, Enqueuer: service.NewJobQueueEnqueuer(jobs, st.Pipelines), Runner: service.NewJobQueueRunner(st, service.NewExecutor(), nil)}
	native, err := wireNativeSourceAutomation(st, &connection.Service{Store: connection.NewMemory()}, source, deps, triggers, nil)
	if err != nil || native == nil || native.Preparer != deps.Runner.SourcePreparer || deps.Runner.NativeAutomation != native || deps.Enqueuer.AdmissionGuard == nil || deps.Enqueuer.PrepareSource == nil || native.Enqueue == nil {
		t.Fatal("source execution not wired coherently", err)
	}
	if p, err := deps.Enqueuer.PrepareSource(context.Background(), &model.Pipeline{}); err != nil || p != nil || source.reads != 0 {
		t.Fatal("unbound pipeline touched source", err)
	}
	if _, err := deps.Enqueuer.PrepareSource(context.Background(), bootNativePipeline()); err == nil || source.reads != 0 {
		t.Fatal("unavailable source connection reached provider", err)
	}
	if n, err := wireNativeSourceAutomation(st, nil, nil, &jobQueueDeps{}, nil, nil); err != nil || n != nil {
		t.Fatal("disabled queue activated source execution", err)
	}
	deps.Runner = nil
	if _, err := wireNativeSourceAutomation(st, &connection.Service{}, source, deps, triggers, nil); err == nil {
		t.Fatal("incomplete worker wiring accepted")
	}
}

func TestNativeAutomationAuthorizesEveryExplicitEnvironmentAsMachineActor(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	for _, id := range []string{"env-a", "env-b"} {
		if err := st.Environments.Create(ctx, &model.Environment{ID: id, Name: strings.ToUpper(id)}); err != nil {
			t.Fatal(err)
		}
	}
	p := bootNativePipeline()
	p.Stages[0].EnvironmentID = "env-a"
	p.Stages = append(p.Stages, model.Stage{ID: "another", Type: model.StageTypeTest, EnvironmentID: "env-b", Config: model.StageConfig{Image: "owned:test"}})
	r := &model.TriggerRule{ID: "rule", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, ActorSubject: "cooker:trigger:rule"}
	var observed []map[string]any
	var mu sync.Mutex
	var allowed atomic.Bool
	allowed.Store(true)
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(observed) }
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if json.NewDecoder(req.Body).Decode(&body) != nil {
			t.Error("invalid authority request")
		}
		mu.Lock()
		observed = append(observed, body)
		mu.Unlock()
		decision := "deny"
		if allowed.Load() {
			decision = "allow"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"decision": decision, "enforced": true})
	}))
	defer provider.Close()
	gov := governance.New(provider.URL, nil, nil).WithDelegateToken("owned-fixture-delegation")
	authorize := nativeAutomationAuthorizer(st, gov)
	if err := authorize(ctx, r, p); err != nil || count() != 2 {
		t.Fatal("did not authorize each explicit environment", err)
	}
	mu.Lock()
	raw, _ := json.Marshal(observed)
	mu.Unlock()
	if !strings.Contains(string(raw), "cooker:trigger:rule") || !strings.Contains(string(raw), "native shop") || strings.Contains(string(raw), "human") {
		t.Fatal("wrong service actor authority")
	}
	before := count()
	r.ActorGroups = []string{"admin"}
	if authorize(ctx, r, p) == nil || count() != before {
		t.Fatal("human group injection reached authority")
	}
	r.ActorGroups = nil
	allowed.Store(false)
	if authorize(ctx, r, p) == nil {
		t.Fatal("provider denial ignored")
	}
	before = count()
	gov.DelegateToken = ""
	if authorize(ctx, r, p) == nil || count() != before {
		t.Fatal("missing delegation bypassed governance")
	}
	gov.DelegateToken = "owned-fixture-delegation"
	p.Stages[1].EnvironmentID = ""
	if authorize(ctx, r, p) == nil || count() != before {
		t.Fatal("missing deployment environment reached authority")
	}
}
