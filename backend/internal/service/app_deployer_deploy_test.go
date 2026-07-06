package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/store/memory"
)

// fakeCloneDockerfile creates a temp dir containing a minimal Dockerfile
// (so buildplan.Detect resolves BuildPlanDockerfile and the single-image
// synthesizePipeline path runs -- no compose parsing, no k8s manifest,
// since the test App's DeployTarget.Kind is left at its zero value).
// Returns a cloneFn closure plus an accessor for the dir it created, so
// the test can assert the defer cleanup actually removed it.
func fakeCloneDockerfile(t *testing.T) (fn func(ctx context.Context, opts github.CloneOptions) (string, error), dirOf func() string, calls func() int) {
	t.Helper()
	var dir string
	var n int
	fn = func(_ context.Context, _ github.CloneOptions) (string, error) {
		n++
		d := t.TempDir()
		// t.TempDir() is auto-cleaned by the test framework; Deploy's
		// own defer os.RemoveAll would then operate on an already
		// gone-or-going directory. Use a manually-managed subdirectory
		// instead, one level under a t.TempDir() parent, so Deploy's
		// defer is the only thing that removes it and we can assert
		// on that independently of the test's own cleanup timing.
		managed := filepath.Join(d, "workdir")
		if err := os.MkdirAll(managed, 0o755); err != nil {
			t.Fatalf("mkdir managed workdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(managed, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
			t.Fatalf("write Dockerfile: %v", err)
		}
		dir = managed
		return managed, nil
	}
	dirOf = func() string { return dir }
	calls = func() int { return n }
	return fn, dirOf, calls
}

func TestAppDeployer_Deploy_HappyPath(t *testing.T) {
	cloneFn, dirOf, _ := fakeCloneDockerfile(t)
	deploys := memory.New().AppDeploys

	d := &AppDeployer{
		Executor: NewExecutor(),
		Registry: "registry.example.com",
		Deploys:  deploys,
		cloneFn:  cloneFn,
	}
	app := &model.App{ID: "app-1", Name: "widgets", GitHubRepo: "acme/widgets", Branch: "main"}

	p, run, err := d.Deploy(context.Background(), app, "run-1", io.Discard)
	if err != nil {
		t.Fatalf("Deploy: unexpected error: %v", err)
	}
	if p == nil || run == nil {
		t.Fatal("expected non-nil pipeline and run")
	}
	if len(p.Stages) != 2 {
		t.Errorf("expected 2 stages (build, push; no k8s deploy target), got %d", len(p.Stages))
	}
	if run.Status != model.RunStatusSuccess {
		t.Errorf("run status = %s, want success", run.Status)
	}

	if dirOf() == "" {
		t.Fatal("fake clone never ran")
	}
	if _, statErr := os.Stat(dirOf()); !os.IsNotExist(statErr) {
		t.Errorf("expected Deploy's defer cleanup to remove workdir %s, stat err = %v", dirOf(), statErr)
	}

	rows, lerr := deploys.ListByApp(context.Background(), "app-1", 10)
	if lerr != nil {
		t.Fatalf("list history: %v", lerr)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 history row, got %d", len(rows))
	}
	if rows[0].Kind != model.AppDeployKindDeploy {
		t.Errorf("history kind = %s, want deploy", rows[0].Kind)
	}
	if rows[0].ImageRef == "" {
		t.Error("expected a non-empty ImageRef on the history row")
	}
}

func TestAppDeployer_Deploy_CloneFailure(t *testing.T) {
	deploys := memory.New().AppDeploys
	wantErr := errors.New("clone boom")
	d := &AppDeployer{
		Executor: NewExecutor(),
		Deploys:  deploys,
		cloneFn: func(_ context.Context, _ github.CloneOptions) (string, error) {
			return "", wantErr
		},
	}
	app := &model.App{ID: "app-1", Name: "widgets", GitHubRepo: "acme/widgets"}

	p, run, err := d.Deploy(context.Background(), app, "run-1", io.Discard)
	if err == nil {
		t.Fatal("expected clone error to propagate")
	}
	if p != nil || run != nil {
		t.Errorf("expected nil pipeline/run on clone failure, got p=%v run=%v", p, run)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want it to wrap %v", err, wantErr)
	}

	rows, lerr := deploys.ListByApp(context.Background(), "app-1", 10)
	if lerr != nil {
		t.Fatalf("list history: %v", lerr)
	}
	if len(rows) != 0 {
		t.Errorf("expected no history row on clone failure, got %d", len(rows))
	}
}

func TestAppDeployer_Deploy_ExecuteFailure(t *testing.T) {
	cloneFn, dirOf, _ := fakeCloneDockerfile(t)
	deploys := memory.New().AppDeploys

	mb := &mockBuilder{err: errors.New("builder boom")}
	d := &AppDeployer{
		Executor: NewExecutor(WithBuilder(mb)),
		Deploys:  deploys,
		cloneFn:  cloneFn,
	}
	app := &model.App{ID: "app-1", Name: "widgets", GitHubRepo: "acme/widgets"}

	p, run, err := d.Deploy(context.Background(), app, "run-1", io.Discard)
	if err == nil {
		t.Fatal("expected execute error to propagate")
	}
	// Per Deploy's documented contract, pipeline+run are still returned
	// non-nil even when execution then fails.
	if p == nil || run == nil {
		t.Fatalf("expected non-nil pipeline/run even on execute failure, got p=%v run=%v", p, run)
	}
	if run.Status != model.RunStatusFailed {
		t.Errorf("run status = %s, want failed", run.Status)
	}

	if _, statErr := os.Stat(dirOf()); !os.IsNotExist(statErr) {
		t.Errorf("expected Deploy's defer cleanup to remove workdir %s even on failure, stat err = %v", dirOf(), statErr)
	}

	rows, lerr := deploys.ListByApp(context.Background(), "app-1", 10)
	if lerr != nil {
		t.Fatalf("list history: %v", lerr)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 history row recorded even on execute failure, got %d", len(rows))
	}
	if rows[0].Status != model.RunStatusFailed {
		t.Errorf("history row status = %s, want failed", rows[0].Status)
	}
}

func TestAppDeployer_Deploy_EmptyGitHubRepo(t *testing.T) {
	cloneFn, _, calls := fakeCloneDockerfile(t)
	d := &AppDeployer{Executor: NewExecutor(), cloneFn: cloneFn}
	app := &model.App{ID: "app-1", Name: "widgets", GitHubRepo: ""}

	p, run, err := d.Deploy(context.Background(), app, "run-1", io.Discard)
	if err == nil {
		t.Fatal("expected an error for empty GitHubRepo")
	}
	if p != nil || run != nil {
		t.Errorf("expected nil pipeline/run, got p=%v run=%v", p, run)
	}
	if calls() != 0 {
		t.Errorf("expected clone to never be attempted, called %d times", calls())
	}
}

// PM26-07-03 pin (service side): the selector this manifest creates is
// immutable for the lifetime of the Deployment object, and the canary
// path re-applies the same-named Deployment — deployer's
// TestCanaryManifest_StableSelectorMatchesNormalDeploy pins the other
// side. If this selector ever changes shape, change both together.
func TestDefaultKubernetesManifest_SelectorIsAppOnly(t *testing.T) {
	app := &model.App{Name: "Shop App"}
	m := defaultKubernetesManifest(app, "reg/shop:v1")
	if want := "matchLabels: {app: shop-app}\n"; !strings.Contains(m, want) {
		t.Errorf("normal-deploy selector must be exactly %q\n---\n%s", want, m)
	}
	if strings.Contains(m, "track") {
		t.Errorf("normal-deploy manifest must not reference track labels\n---\n%s", m)
	}
}
