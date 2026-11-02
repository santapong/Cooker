package service

import (
	"context"
	"encoding/json"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store/memory"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type nativeSourceFixture struct {
	base, commit, object, repositoryID string
	files                              map[string]string
	identities, revisions, checkouts   int
	lastCheckout                       string
}

func (f *nativeSourceFixture) Identity(_ context.Context, c *connection.Connection, repo string) (model.GitSourceIdentity, error) {
	f.identities++
	return model.GitSourceIdentity{Provider: c.Provider, ConnectionID: c.ID, Host: "github.com", RepositoryID: f.repositoryID, Repository: repo}, nil
}
func (f *nativeSourceFixture) Revision(_ context.Context, _ *connection.Connection, _, ref string) (SourceRevision, error) {
	f.revisions++
	return SourceRevision{FullRef: ref, Commit: f.commit, Object: f.object}, nil
}
func (f *nativeSourceFixture) CheckoutSource(_ context.Context, p *model.PipelineSourcePlan, _ io.Writer) (string, error) {
	f.checkouts++
	root, err := os.MkdirTemp(f.base, "checkout-")
	if err != nil {
		return "", err
	}
	f.lastCheckout = root
	files := map[string]string{".git/fixture-head": p.CommitSHA}
	for k, v := range f.files {
		files[k] = v
	}
	for name, value := range files {
		path := filepath.Join(root, name)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
		if err = os.WriteFile(path, []byte(value), 0600); err != nil {
			return "", err
		}
	}
	return root, nil
}
func nativeSourceSetup(t *testing.T) (*PipelineSourcePreparer, *model.Pipeline, *nativeSourceFixture) {
	t.Helper()
	ctx := context.Background()
	st := memory.New()
	cs := connection.NewMemory()
	c := &connection.Connection{ID: "native-connection", Provider: "github", Origin: "https://github.com", APIOrigin: "https://api.github.com", Enabled: true, Version: 1, Repositories: []string{"owned/native"}}
	if err := cs.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	f := &nativeSourceFixture{base: t.TempDir(), commit: strings.Repeat("a", 40), object: strings.Repeat("a", 40), repositoryID: "provider-repo", files: map[string]string{"docker/api.Dockerfile": "FROM scratch\nCOPY . /app\n", "api/commit.txt": "exact-commit-content"}}
	p := &model.Pipeline{ID: "native-pipeline", Name: "native", Version: 1, Source: &model.PipelineSourceConfig{ConnectionID: c.ID, Repository: "owned/native", FullRef: "refs/heads/main"}, Stages: []model.Stage{{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: "api", Dockerfile: "../docker/api.Dockerfile"}}}}
	p.SetDefaults()
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	return &PipelineSourcePreparer{Store: st, Connections: &connection.Service{Store: cs}, Source: f, Head: func(_ context.Context, root string) (string, error) {
		b, err := os.ReadFile(filepath.Join(root, ".git/fixture-head"))
		return string(b), err
	}}, p, f
}

func TestNativeSourceBuildPathsConsumeExportAndDoNotMutateDefinition(t *testing.T) {
	s, p, f := nativeSourceSetup(t)
	ctx := context.Background()
	original, _ := json.Marshal(p)
	plan, err := s.Prepare(ctx, p, "", "manual")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(f.lastCheckout); !os.IsNotExist(err) {
		t.Fatal("review checkout was retained")
	}
	prepared, err := s.Open(ctx, p, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	root := prepared.Root
	defer prepared.Close()
	contextDir, df, err := PipelineBuildPaths(root, &p.Stages[0])
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(contextDir, "commit.txt"))
	if err != nil || string(b) != "exact-commit-content" {
		t.Fatal("accepted source bytes not consumed", err)
	}
	if df != "../docker/api.Dockerfile" {
		t.Fatal(df)
	}
	if _, err = os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatal("Git metadata exported")
	}
	after, _ := json.Marshal(p)
	if string(after) != string(original) {
		t.Fatal("source preparation rewrote authored host/source paths")
	}
	if err = prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("native export was retained")
	}
}

func TestNativeSourceValidationBoundaries(t *testing.T) {
	_, p, _ := nativeSourceSetup(t)
	cases := []struct {
		name string
		edit func(*model.Pipeline)
	}{{"absolute", func(p *model.Pipeline) { p.Stages[0].Config.Context = "/etc" }}, {"context escape", func(p *model.Pipeline) { p.Stages[0].Config.Context = "../host" }}, {"dockerfile escape", func(p *model.Pipeline) { p.Stages[0].Config.Dockerfile = "../../etc/passwd" }}, {"output path", func(p *model.Pipeline) { p.Stages[0].Config.Context = "${stages.other.path}" }}, {"remote", func(p *model.Pipeline) { p.Stages[0].Config.Context = "https://example.test/repo" }}, {"metadata", func(p *model.Pipeline) { p.Stages[0].Config.Context = ".git" }}, {"review only", func(p *model.Pipeline) { p.Stages[0].Config.ReviewOnly = true }}, {"compose", func(p *model.Pipeline) {
		p.Stages = append(p.Stages, model.Stage{ID: "deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{DeployRuntime: "compose"}})
	}}, {"gitops", func(p *model.Pipeline) {
		p.Stages = append(p.Stages, model.Stage{ID: "gitops", Type: model.StageTypeGitOpsCommit})
	}}, {"untyped ref", func(p *model.Pipeline) { p.Source.FullRef = "main" }}, {"registry duplicate", func(p *model.Pipeline) { p.Stages[0].Config.RegistryConnectionIDs = []string{"registry", "registry"} }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(p)
			var cp model.Pipeline
			json.Unmarshal(b, &cp)
			tc.edit(&cp)
			if ValidatePipelineSource(&cp) == nil {
				t.Fatal("unsafe source definition accepted")
			}
		})
	}
	legacy := *p
	legacy.Source = nil
	legacy.Stages = []model.Stage{{Type: model.StageTypeBuild, Config: model.StageConfig{Context: "/existing/host"}}}
	if err := ValidatePipelineSource(&legacy); err != nil {
		t.Fatal("legacy source-unbound semantics changed", err)
	}
}

func TestNativeSourceRecipePresenceAndCodeRevision(t *testing.T) {
	s, p, f := nativeSourceSetup(t)
	ctx := context.Background()
	one, err := s.Prepare(ctx, p, "", "automatic")
	if err != nil {
		t.Fatal(err)
	}
	f.commit = strings.Repeat("b", 40)
	f.object = f.commit
	f.files["api/commit.txt"] = "new-application-code"
	two, err := s.Prepare(ctx, p, "", "automatic")
	if err != nil || two.EnvelopeHash != one.EnvelopeHash || two.PlanHash != one.PlanHash {
		t.Fatal("ordinary source changes altered recipe approval", err)
	}
	a, _ := PipelineSourceDigest(one)
	b, _ := PipelineSourceDigest(two)
	if a == b {
		t.Fatal("different accepted commits reused source-plan digest")
	}
	f.files["api/.dockerignore"] = "private\n"
	three, err := s.Prepare(ctx, p, "", "automatic")
	if err != nil || three.EnvelopeHash == two.EnvelopeHash {
		t.Fatal("new ignore file did not change recipe envelope", err)
	}
	if _, err = s.Open(ctx, p, two, false); err == nil {
		t.Fatal("changed reviewed recipe executed")
	}
	if _, err = os.Stat(f.lastCheckout); !os.IsNotExist(err) {
		t.Fatal("failed source checkout was retained")
	}
}

func TestNativeSourcePoisonPlanFailsBeforeProviderReads(t *testing.T) {
	s, p, f := nativeSourceSetup(t)
	ctx := context.Background()
	plan, err := s.Prepare(ctx, p, "", "manual")
	if err != nil {
		t.Fatal(err)
	}
	plan.EnvironmentVersions["OTHER"] = 1
	plan.PlanHash = pipelineSourcePlanHash(p, plan)
	before := f.identities
	if err = s.Validate(ctx, p, plan, false); err == nil || f.identities != before {
		t.Fatal("extra environment plan reached provider", err)
	}
}

func TestNativeSourceRejectsRegistryEnvironmentsBeforeProviderReads(t *testing.T) {
	for _, name := range []string{"_registries", "registry_saved", "_registries (internal)", "  REGISTRY_saved  "} {
		t.Run(name, func(t *testing.T) {
			s, p, f := nativeSourceSetup(t)
			ctx := context.Background()
			env := &model.Environment{ID: "normal-environment", Name: "normal"}
			if err := s.Store.Environments.Create(ctx, env); err != nil {
				t.Fatal(err)
			}
			p.Stages[0].EnvironmentID = env.ID
			plan, err := s.Prepare(ctx, p, "", "manual")
			if err != nil {
				t.Fatal(err)
			}
			env.Name = name
			if err = s.Store.Environments.Update(ctx, env); err != nil {
				t.Fatal(err)
			}
			before := f.identities + f.revisions + f.checkouts
			if _, err = s.Prepare(ctx, p, "", "manual"); err == nil {
				t.Fatal("registry environment acquired a source plan")
			}
			if err = s.Validate(ctx, p, plan, false); err == nil {
				t.Fatal("registry environment validated an old source plan")
			}
			if f.identities+f.revisions+f.checkouts != before {
				t.Fatal("registry credential environment reached provider or checkout")
			}
		})
	}
	s, p, f := nativeSourceSetup(t)
	p.Stages[0].EnvironmentID = "  REGISTRY_private  "
	if _, err := s.Prepare(context.Background(), p, "", "manual"); err == nil || f.identities+f.revisions+f.checkouts != 0 {
		t.Fatal("reserved ID reached a store/provider", err)
	}
}

func TestNativeSourceRevocationRefAndIdentityChecks(t *testing.T) {
	for _, kind := range []string{"disabled", "version", "ref", "repository", "object"} {
		t.Run(kind, func(t *testing.T) {
			s, p, f := nativeSourceSetup(t)
			ctx := context.Background()
			plan, err := s.Prepare(ctx, p, "", "automatic")
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "disabled", "version":
				c, _ := s.Connections.Store.Get(ctx, p.Source.ConnectionID)
				if kind == "disabled" {
					c.Enabled = false
				}
				s.Connections.Store.Update(ctx, c)
			case "ref":
				f.commit = strings.Repeat("b", 40)
			case "object":
				f.object = strings.Repeat("b", 40)
			case "repository":
				f.repositoryID = "replaced-repository"
			}
			before := f.checkouts
			if _, err = s.Open(ctx, p, plan, true); err == nil || f.checkouts != before {
				t.Fatal("revoked/changed source checked out", err)
			}
		})
	}
}

func TestNativeSourceExportRejectsLinksSpecialFilesAndSize(t *testing.T) {
	for _, kind := range []string{"symlink", "size", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			ctx := context.Background()
			switch kind {
			case "symlink":
				os.Symlink("/etc/passwd", filepath.Join(root, "escape"))
			case "size":
				file, err := os.Create(filepath.Join(root, "large"))
				if err != nil {
					t.Fatal(err)
				}
				file.Truncate((1 << 30) + 1)
				file.Close()
			case "cancel":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if out, err := ExportPipelineSource(ctx, root); err == nil || out != "" {
				t.Fatal("invalid export survived", out, err)
			}
		})
	}
}

func TestNativePipelineSourceYAMLRoundTrip(t *testing.T) {
	_, p, _ := nativeSourceSetup(t)
	p.Stages[0].Config.RegistryConnectionIDs = []string{"scoped-registry"}
	b, err := ExportPipelineYAML(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ImportPipelineYAML(b)
	if err != nil || got.Source == nil || *got.Source != *p.Source || len(got.Stages[0].Config.RegistryConnectionIDs) != 1 {
		t.Fatal("source/registry selection lost", err)
	}
}

func TestNativePipelineSourceContextRequiresVerifiedCheckout(t *testing.T) {
	s, p, _ := nativeSourceSetup(t)
	ctx := context.Background()
	if RequirePipelineSourceContext(ctx, p) == nil {
		t.Fatal("source-bound execution fell back to host paths")
	}
	plan, err := s.Prepare(ctx, p, "", "manual")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := s.Open(ctx, p, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	bound := WithPipelineSourceCheckout(ctx, prepared)
	if err = RequirePipelineSourceContext(bound, p); err != nil {
		t.Fatal(err)
	}
	root, _, err := PipelineStageBuildPaths(bound, &p.Stages[0])
	if err != nil || !strings.HasPrefix(root, prepared.Root) {
		t.Fatal("bound builder path did not use export", err)
	}
	legacy := *p
	legacy.Source = nil
	if RequirePipelineSourceContext(bound, &legacy) == nil {
		t.Fatal("native source checkout attached to unrelated pipeline")
	}
	prepared.Close()
	if RequirePipelineSourceContext(bound, p) == nil {
		t.Fatal("closed source context remained executable")
	}
}
