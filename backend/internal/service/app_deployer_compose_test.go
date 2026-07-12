package service

import (
	"sort"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

// stageByID returns the synthesized stage with the given ID, or nil.
func stageByID(p *model.Pipeline, id string) *model.Stage {
	for i := range p.Stages {
		if p.Stages[i].ID == id {
			return &p.Stages[i]
		}
	}
	return nil
}

// edgeSet returns "src->tgt" keys for all edges.
func edgeSet(p *model.Pipeline) map[string]bool {
	m := make(map[string]bool, len(p.Edges))
	for _, e := range p.Edges {
		m[e.Source+"->"+e.Target] = true
	}
	return m
}

func k8sApp() *model.App {
	return &model.App{
		ID:   "a1",
		Name: "shop",
		DeployTarget: model.DeployTarget{
			Kind:      model.DeployTargetKubernetes,
			Namespace: "prod",
		},
	}
}

// Test the core synthesis: build-only-for-build-services, intra-service
// build→push→deploy chains, cross-service deploy edges from depends_on,
// per-stage Group, and resources carried onto the deploy stage.
func TestSynthesizeFromCompose_PerServiceDAG(t *testing.T) {
	graph := &model.ComposeGraph{
		Services: []model.ComposeService{
			{
				Name:      "web",
				Build:     &model.ComposeBuild{Context: "./web", Dockerfile: "Dockerfile"},
				DependsOn: []string{"api"},
				Group:     "frontend",
				Ports:     []string{"8080:80"},
			},
			{
				Name:      "api",
				Build:     &model.ComposeBuild{Context: "./api"},
				DependsOn: []string{"db"},
				Group:     "backend",
				Resources: &model.ResourceLimits{Memory: "512m", CPUs: "1.5"},
			},
			{
				Name:  "db",
				Image: "postgres:16",
				Group: "backend",
			},
		},
	}

	p, run, err := synthesizePipelineFromCompose(k8sApp(), graph, "/work", "reg.example.com", 1700, "app-a1-1700", "run-1", nil, synthOpts{})
	if err != nil {
		t.Fatalf("synth error: %v", err)
	}

	// web + api build → 2 build + 2 push + 3 deploy = 7 stages.
	if len(p.Stages) != 7 {
		ids := make([]string, len(p.Stages))
		for i, s := range p.Stages {
			ids[i] = s.ID
		}
		sort.Strings(ids)
		t.Fatalf("expected 7 stages, got %d: %v", len(p.Stages), ids)
	}

	// db is image-only → no build/push, but has a deploy.
	if stageByID(p, "build-db") != nil || stageByID(p, "push-db") != nil {
		t.Errorf("db must not have build/push stages")
	}
	if stageByID(p, "deploy-db") == nil {
		t.Errorf("db must have a deploy stage")
	}

	// Intra-service chains + cross-service deploy ordering.
	es := edgeSet(p)
	want := []string{
		"build-web->push-web", "push-web->deploy-web",
		"build-api->push-api", "push-api->deploy-api",
		"deploy-api->deploy-web", // web depends_on api
		"deploy-db->deploy-api",  // api depends_on db
	}
	for _, w := range want {
		if !es[w] {
			t.Errorf("missing edge %q; have %v", w, es)
		}
	}

	// Group carried onto stages.
	if s := stageByID(p, "deploy-web"); s == nil || s.Group != "frontend" {
		t.Errorf("deploy-web group: got %+v", s)
	}
	if s := stageByID(p, "build-api"); s == nil || s.Group != "backend" {
		t.Errorf("build-api group: got %+v", s)
	}

	// Resources on the api deploy stage; manifest carries the limits.
	api := stageByID(p, "deploy-api")
	if api == nil || api.Config.Resources == nil || api.Config.Resources.Memory != "512m" {
		t.Fatalf("deploy-api resources: got %+v", api)
	}
	if api.Config.DeployRuntime != "kubernetes" {
		t.Errorf("deploy-api runtime: got %q want kubernetes", api.Config.DeployRuntime)
	}
	if !strings.Contains(api.Config.ManifestPath, "memory: \"512Mi\"") ||
		!strings.Contains(api.Config.ManifestPath, "cpu: \"1.5\"") {
		t.Errorf("deploy-api manifest missing resource limits:\n%s", api.Config.ManifestPath)
	}

	// Per-service image tag uses app-<svc> scheme.
	web := stageByID(p, "build-web")
	if web == nil || len(web.Config.Tags) != 1 || web.Config.Tags[0] != "reg.example.com/shop-web:1700" {
		t.Errorf("build-web tag: got %+v", web)
	}

	// One pending StageRun per stage.
	if len(run.StageRuns) != len(p.Stages) {
		t.Errorf("stage runs: got %d want %d", len(run.StageRuns), len(p.Stages))
	}
	if run.PipelineID != "app-a1-1700" || p.ID != "app-a1-1700" {
		t.Errorf("pipeline id mismatch: p=%s run.PipelineID=%s", p.ID, run.PipelineID)
	}
	// Run ID aligns with the caller-supplied runID (stub row / WS channel).
	if run.ID != "run-1" {
		t.Errorf("run id: got %q want run-1", run.ID)
	}

	// web's deploy stage carries its compose ports for the docker-run
	// path (K8s uses the manifest, but the data must be present either
	// way so a docker-host deploy publishes them).
	webDeploy := stageByID(p, "deploy-web")
	if webDeploy == nil || len(webDeploy.Config.ComposePorts) != 1 || webDeploy.Config.ComposePorts[0] != "8080:80" {
		t.Errorf("deploy-web ComposePorts: got %+v", webDeploy.Config.ComposePorts)
	}
}

// Edge IDs must be unique even when service slugs contain dashes — the
// old "e-dep-<dep>-<svc>" format was ambiguous (distinct pairs could
// collide), which would drop edges in the xyflow canvas (duplicate
// React keys). Regression guard for the audit HIGH finding.
func TestSynthesizeFromCompose_UniqueEdgeIDs(t *testing.T) {
	graph := &model.ComposeGraph{
		Services: []model.ComposeService{
			{Name: "a-b", Image: "x"},
			{Name: "c", Image: "y", DependsOn: []string{"a-b"}},
			{Name: "a", Image: "z"},
			{Name: "b-c", Image: "w", DependsOn: []string{"a"}},
		},
	}
	p, _, err := synthesizePipelineFromCompose(k8sApp(), graph, "/w", "reg", 1, "p", "", nil, synthOpts{})
	if err != nil {
		t.Fatalf("synth error: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range p.Edges {
		if seen[e.ID] {
			t.Errorf("duplicate edge ID %q (source=%s target=%s)", e.ID, e.Source, e.Target)
		}
		seen[e.ID] = true
	}
	// Two cross-service edges, both unique.
	if len(p.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(p.Edges))
	}
}

// Env + ports from a compose service must reach the docker deploy stage
// so a docker-host deploy injects/publishes them (regression guard for
// the port/env carry-through fix).
func TestSynthesizeFromCompose_DockerCarriesPortsAndEnv(t *testing.T) {
	app := &model.App{
		ID:           "d2",
		Name:         "edge",
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost},
	}
	graph := &model.ComposeGraph{
		Services: []model.ComposeService{
			{
				Name:        "api",
				Image:       "nginx",
				Ports:       []string{"8080:80", "9090"},
				Environment: map[string]string{"LOG_LEVEL": "debug"},
			},
		},
	}
	p, _, err := synthesizePipelineFromCompose(app, graph, "/w", "reg", 1, "p", "", nil, synthOpts{})
	if err != nil {
		t.Fatalf("synth error: %v", err)
	}
	d := stageByID(p, "deploy-api")
	if d == nil {
		t.Fatal("deploy-api missing")
	}
	if len(d.Config.ComposePorts) != 2 || d.Config.ComposePorts[0] != "8080:80" {
		t.Errorf("ComposePorts: got %+v", d.Config.ComposePorts)
	}
	if d.Config.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("Env not carried: got %+v", d.Config.Env)
	}
	// And the executor maps ComposePorts → publishable ports.
	if got := composePortsToPublish(d.Config); len(got) != 2 {
		t.Errorf("composePortsToPublish: got %+v", got)
	}
}

// depends_on cycles must be rejected with a friendly error, not panic.
func TestSynthesizeFromCompose_CycleRejected(t *testing.T) {
	graph := &model.ComposeGraph{
		Services: []model.ComposeService{
			{Name: "a", Image: "x", DependsOn: []string{"b"}},
			{Name: "b", Image: "y", DependsOn: []string{"a"}},
		},
	}
	_, _, err := synthesizePipelineFromCompose(k8sApp(), graph, "/work", "reg", 1, "p", "", nil, synthOpts{})
	if err == nil {
		t.Fatalf("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected an 'invalid' graph error, got %v", err)
	}
}

// docker-host target selects the docker deploy runtime and skips
// manifest synthesis (the docker deployer builds the run command).
func TestSynthesizeFromCompose_DockerRuntime(t *testing.T) {
	app := &model.App{
		ID:           "d1",
		Name:         "edge",
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost},
	}
	graph := &model.ComposeGraph{
		Services: []model.ComposeService{{Name: "svc", Image: "nginx"}},
	}
	p, _, err := synthesizePipelineFromCompose(app, graph, "/w", "reg", 1, "p", "", nil, synthOpts{})
	if err != nil {
		t.Fatalf("synth error: %v", err)
	}
	d := stageByID(p, "deploy-svc")
	if d == nil || d.Config.DeployRuntime != "docker" {
		t.Fatalf("expected docker runtime, got %+v", d)
	}
	if d.Config.ManifestPath != "" {
		t.Errorf("docker deploy must not synthesize a manifest, got %q", d.Config.ManifestPath)
	}
	if d.Config.Image != "nginx" {
		t.Errorf("image-only deploy image: got %q", d.Config.Image)
	}
}

// firstContainerPort parsing edge cases.
func TestFirstContainerPort(t *testing.T) {
	cases := map[string]int{}
	cases["only default"] = firstContainerPort(nil)
	if cases["only default"] != 80 {
		t.Errorf("nil ports → %d want 80", cases["only default"])
	}
	if got := firstContainerPort([]string{"8080:3000"}); got != 3000 {
		t.Errorf("8080:3000 → %d want 3000", got)
	}
	if got := firstContainerPort([]string{"5432"}); got != 5432 {
		t.Errorf("5432 → %d want 5432", got)
	}
	if got := firstContainerPort([]string{"443:443/tcp"}); got != 443 {
		t.Errorf("443:443/tcp → %d want 443", got)
	}
}
