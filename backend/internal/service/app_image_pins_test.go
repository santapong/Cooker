package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
	"github.com/santapong/cooker/internal/source/gittransport"
	"github.com/santapong/cooker/internal/store/memory"
	"gopkg.in/yaml.v3"
)

type imagePinDeployFunc func(context.Context, deployer.Request) (deployer.Result, error)

func (f imagePinDeployFunc) Deploy(ctx context.Context, req deployer.Request) (deployer.Result, error) {
	return f(ctx, req)
}

func imagePinSource(t *testing.T, content string) (string, func(context.Context, github.CloneOptions) (string, error)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "compose.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Env = gittransport.Environment("/nonexistent")
		b, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(b)), err
	}
	for _, args := range [][]string{{"init", "--initial-branch=main", repo}, {"-C", repo, "add", "compose.yaml"}, {"-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "image fixture"}} {
		if out, err := run(context.Background(), args...); err != nil {
			t.Fatalf("git fixture: %v %s", err, out)
		}
	}
	commit, err := run(context.Background(), "-C", repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return commit, func(ctx context.Context, opts github.CloneOptions) (string, error) {
		dir, err := os.MkdirTemp("", "cooker-image-pin-test-*")
		if err != nil {
			return "", err
		}
		if _, err = run(ctx, "-c", "protocol.file.allow=always", "clone", "--no-local", repo, dir); err == nil {
			_, err = run(ctx, "-C", dir, "checkout", "--detach", opts.Commit)
		}
		if err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
		return dir, nil
	}
}

func TestAppQueuePinsRegistryImageBeforeAcceptanceAndNeverResolvesAtExecution(t *testing.T) {
	ctx := context.Background()
	reg := registry.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "fixture" || p != "image-fixture-password" {
			w.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		reg.ServeHTTP(w, r)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	ref, _ := name.NewTag(host + "/images/app:current")
	auth := authn.FromConfig(authn.AuthConfig{Username: "fixture", Password: "image-fixture-password"})
	original, err := mutate.AppendLayers(empty.Image, static.NewLayer([]byte("accepted image"), types.OCILayer))
	if err != nil {
		t.Fatal(err)
	}
	// Pin an index, proving admission does not select the control host's platform.
	index := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: original})
	if err := remote.WriteIndex(ref, index, remote.WithAuth(auth)); err != nil {
		t.Fatal(err)
	}
	digest, _ := index.Digest()
	want := ref.Context().Digest(digest.String()).Name()
	sha, clone := imagePinSource(t, "services:\n  web:\n    image: ${APP_IMAGE}\n")
	st, jobs := memory.New(), jobqueue.NewMemory()
	env := &model.Environment{ID: "images", Name: "test", PlainVars: map[string]string{"APP_IMAGE": ref.Name()}}
	if err := st.Environments.Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	app := &model.App{ID: "image-app", Name: "image-app", GitHubRepo: "acme/app", Branch: "main", EnvironmentID: env.ID, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Files: []string{"compose.yaml"}}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	calls := 0
	adapter := imagePinDeployFunc(func(_ context.Context, req deployer.Request) (deployer.Result, error) {
		calls++
		if req.Image != want {
			t.Errorf("runtime received %q, want %q", req.Image, want)
		}
		data, err := os.ReadFile(req.ComposeFile)
		if err != nil {
			return deployer.Result{}, err
		}
		var project struct {
			Services map[string]struct {
				Image string `yaml:"image"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &project); err != nil {
			return deployer.Result{}, err
		}
		if project.Services["web"].Image != want {
			t.Error("runtime Compose retained a mutable tag")
		}
		return deployer.Result{}, nil
	})
	d := &AppDeployer{cloneFn: clone, EnvResolver: &AppEnvResolver{Environments: st.Environments}, Executor: NewExecutor(WithComposeDeployer(adapter)), SavePipeline: st.Pipelines.Update}
	resolverCalls := 0
	resolve := RegistryImageResolver(func(_ context.Context, requestedHost string) (authn.Authenticator, error) {
		resolverCalls++
		if requestedHost != host {
			return nil, fmt.Errorf("wrong credential host")
		}
		return auth, nil
	})
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, RequireImagePins: true}
	q.PrepareImages = func(ctx context.Context, plan *AppDeployPlan) (*AppImageResolution, error) {
		return d.PrepareImages(ctx, plan, resolve)
	}
	receipt, err := q.Enqueue(ctx, app, sha, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, _ := jobs.Get(ctx, receipt.JobID)
	if strings.Contains(string(job.Payload), "image-fixture-password") {
		t.Fatal("registry credential was persisted")
	}
	var plan AppDeployPlan
	if err := json.Unmarshal(job.Payload, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Images.Services["web"].Image != want {
		t.Fatal("accepted job is missing image evidence")
	}
	if err := remote.Write(ref, empty.Image, remote.WithAuth(auth)); err != nil {
		t.Fatal(err)
	}
	if err := q.Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
	if resolverCalls != 1 || calls != 1 {
		t.Fatalf("resolver=%d deployment=%d", resolverCalls, calls)
	}
	run, _ := st.Runs.Get(ctx, receipt.RunID)
	if run.Status != model.RunStatusSuccess {
		t.Fatalf("run did not succeed: %+v", run)
	}
	graph, _ := st.Pipelines.Get(ctx, receipt.PipelineID)
	if stageByID(graph, "deploy-web").Config.Image != want {
		t.Fatal("saved DAG is not pinned")
	}

	// Anonymous mode must not use a populated host Docker login.
	dockerConfig := t.TempDir()
	encoded := base64.StdEncoding.EncodeToString([]byte("fixture:image-fixture-password"))
	_ = os.WriteFile(filepath.Join(dockerConfig, "config.json"), []byte(fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`, host, encoded)), 0600)
	t.Setenv("DOCKER_CONFIG", dockerConfig)
	if _, err := RegistryImageResolver(nil)(ctx, ref.Name()); err == nil {
		t.Fatal("anonymous image resolution used host Docker credentials")
	}
}

func TestAppImageResolutionRejectsMissingOrMismatchedPinsBeforeMutation(t *testing.T) {
	for _, mode := range []string{"missing", "wrong repository", "wrong source", "mutable result", "extra service", "already pinned changed"} {
		t.Run(mode, func(t *testing.T) {
			app := inspectionApp()
			app.BuildPlan.Files = []string{"compose.yaml"}
			image := "alpine:3"
			pin := AppImagePin{Requested: "index.docker.io/library/alpine:3", Image: "index.docker.io/library/alpine@sha256:" + strings.Repeat("a", 64)}
			pins := &AppImageResolution{Version: 1, Kind: model.BuildPlanCompose, Services: map[string]AppImagePin{"web": pin}}
			switch mode {
			case "missing":
				delete(pins.Services, "web")
			case "wrong repository":
				pin.Image = "index.docker.io/library/other@sha256:" + strings.Repeat("a", 64)
				pins.Services["web"] = pin
			case "wrong source":
				pin.Requested = "index.docker.io/library/alpine:4"
				pins.Services["web"] = pin
			case "mutable result":
				pin.Image = "alpine:4"
				pins.Services["web"] = pin
			case "extra service":
				pins.Services["other"] = pin
			case "already pinned changed":
				image = "alpine@sha256:" + strings.Repeat("b", 64)
				pin.Requested = "index.docker.io/library/" + image
				pins.Services["web"] = pin
			}
			root := inspectionFixture(t, map[string]string{"compose.yaml": "services:\n  web:\n    image: " + image + "\n"})
			resolved, err := loadRepositoryCompose(context.Background(), root, app, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), appImageResolutionKey{}, pins)
			if err := applyAppImageResolution(ctx, model.BuildPlanCompose, resolved); err == nil {
				t.Fatal("unsafe pin accepted")
			}
		})
	}
}

func TestAppImageAdmissionFailsClosedWithoutDurableJob(t *testing.T) {
	ctx := context.Background()
	sha, clone := imagePinSource(t, "services:\n  web: {image: alpine:3}\n")
	for _, mode := range []string{"missing preparer", "registry unavailable", "environment changes", "wrong digest repository"} {
		t.Run(mode, func(t *testing.T) {
			st, jobs := memory.New(), jobqueue.NewMemory()
			env := &model.Environment{ID: "env", Name: "env", PlainVars: map[string]string{}}
			if err := st.Environments.Create(ctx, env); err != nil {
				t.Fatal(err)
			}
			app := &model.App{ID: "app", Name: "app", GitHubRepo: "acme/app", Branch: "main", EnvironmentID: env.ID, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, Files: []string{"compose.yaml"}}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
			d := &AppDeployer{cloneFn: clone, EnvResolver: &AppEnvResolver{Environments: st.Environments}}
			q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, RequireImagePins: true}
			if mode != "missing preparer" {
				q.PrepareImages = func(ctx context.Context, p *AppDeployPlan) (*AppImageResolution, error) {
					return d.PrepareImages(ctx, p, func(context.Context, string) (string, error) {
						switch mode {
						case "registry unavailable":
							return "", fmt.Errorf("fixture unavailable")
						case "environment changes":
							env.PlainVars["NEW"] = "value"
							if err := st.Environments.Update(ctx, env); err != nil {
								t.Fatal(err)
							}
						case "wrong digest repository":
							return "other@sha256:" + strings.Repeat("a", 64), nil
						}
						return "alpine@sha256:" + strings.Repeat("a", 64), nil
					})
				}
			}
			if _, err := q.Enqueue(ctx, app, sha, nil); err == nil {
				t.Fatal("unsafe job accepted")
			}
			stats, _ := jobs.Stats(ctx)
			if stats[jobqueue.StatusPending] != 0 {
				t.Fatal("failed admission left runnable work")
			}
		})
	}
}

func TestImageDigestAdmissionDoesNotRequireRegistryNetwork(t *testing.T) {
	called := false
	resolve := RegistryImageResolver(func(context.Context, string) (authn.Authenticator, error) {
		called = true
		return nil, fmt.Errorf("unavailable")
	})
	want := "unavailable.invalid/team/app@sha256:" + strings.Repeat("a", 64)
	got, err := resolve(context.Background(), want)
	if err != nil || got != want || called {
		t.Fatalf("already pinned image unexpectedly accessed registry: %s %v %v", got, err, called)
	}
}

func TestAppQueueRejectsMissingImageEvidenceBeforeClone(t *testing.T) {
	ctx := context.Background()
	st, jobs := memory.New(), jobqueue.NewMemory()
	app := &model.App{ID: "app", Name: "app", GitHubRepo: "acme/app", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
	if err := st.Apps.Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	d := &AppDeployer{cloneFn: func(context.Context, github.CloneOptions) (string, error) {
		t.Fatal("old job reached source execution")
		return "", fmt.Errorf("unexpected")
	}}
	q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d}
	r, err := q.Enqueue(ctx, app, strings.Repeat("a", 40), nil)
	if err != nil {
		t.Fatal(err)
	}
	q.RequireImagePins = true
	job, _ := jobs.Get(ctx, r.JobID)
	if err := q.Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
	run, _ := st.Runs.Get(ctx, r.RunID)
	if run.Status != model.RunStatusFailed || !strings.Contains(run.Error, "image resolution") {
		t.Fatal("old job was not rejected", run)
	}
}

func TestSourceBuildAdmissionChecksCapabilityAndEnvironmentBeforeQueue(t *testing.T) {
	ctx := context.Background()
	sha, clone := imagePinSource(t, "FROM scratch\n")
	for _, mode := range []string{"supported Dockerfile", "unsupported capability", "unsupported buildpack", "environment drift"} {
		t.Run(mode, func(t *testing.T) {
			st, jobs := memory.New(), jobqueue.NewMemory()
			env := &model.Environment{ID: "env", Name: "env", PlainVars: map[string]string{}}
			if err := st.Environments.Create(ctx, env); err != nil {
				t.Fatal(err)
			}
			app := &model.App{ID: "app", Name: "app", GitHubRepo: "acme/app", EnvironmentID: "env", BuildPlan: &model.BuildPlan{Kind: model.BuildPlanDockerfile, Path: "compose.yaml"}, DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}}
			if mode == "unsupported buildpack" {
				app.BuildPlan.Kind = model.BuildPlanBuildpack
			}
			buildChecked := false
			d := &AppDeployer{cloneFn: clone, EnvResolver: &AppEnvResolver{Environments: st.Environments}, CheckExecution: func(_ *model.App, build bool) error {
				if !build {
					return nil
				}
				buildChecked = true
				if mode == "unsupported capability" {
					return fmt.Errorf("builder cannot publish")
				}
				if mode == "environment drift" {
					env.PlainVars["CHANGED"] = "yes"
					return st.Environments.Update(ctx, env)
				}
				return nil
			}}
			q := &AppDeployQueue{Jobs: jobs, Store: st, Deployer: d, RequireImagePins: true}
			q.PrepareImages = func(ctx context.Context, plan *AppDeployPlan) (*AppImageResolution, error) {
				return d.PrepareImages(ctx, plan, nil)
			}
			_, err := q.Enqueue(ctx, app, sha, nil)
			if (err == nil) != (mode == "supported Dockerfile") {
				t.Fatalf("mode %s: %v", mode, err)
			}
			if mode != "unsupported buildpack" && !buildChecked {
				t.Fatal("source build capability not checked")
			}
			stats, _ := jobs.Stats(ctx)
			if mode != "supported Dockerfile" && stats[jobqueue.StatusPending] != 0 {
				t.Fatal("unsupported source build was queued")
			}
		})
	}
}
