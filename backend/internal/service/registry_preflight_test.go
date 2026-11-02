package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type preflightRegistryStore struct {
	store.RegistryConfigStore
	reads  int
	change func(*model.RegistryConfig) (*model.RegistryConfig, error)
}

func (s *preflightRegistryStore) Get(ctx context.Context, id string) (*model.RegistryConfig, error) {
	s.reads++
	r, err := s.RegistryConfigStore.Get(ctx, id)
	if err != nil || s.change == nil {
		return r, err
	}
	return s.change(r)
}

type preflightEnvironmentStore struct {
	store.EnvironmentStore
	reads int
}

func (s *preflightEnvironmentStore) Get(ctx context.Context, id string) (*model.Environment, error) {
	s.reads++
	return nil, errors.New(registryCanary)
}

func preflightApp(ids ...string) *model.App {
	return &model.App{ID: "app-a", EnvironmentID: "runtime", DeployTarget: model.DeployTarget{Kind: model.DeployTargetDockerHost}, BuildPlan: &model.BuildPlan{Kind: model.BuildPlanCompose, RegistryConnectionIDs: ids}}
}
func preflightPipeline(ids ...string) *model.Pipeline {
	return &model.Pipeline{ID: "pipe-a", Stages: []model.Stage{{ID: "build", EnvironmentID: "runtime", Type: model.StageTypeBuild, Config: model.StageConfig{RegistryConnectionIDs: ids}}, {ID: "deploy", Type: model.StageTypeDeploy, Config: model.StageConfig{DeployRuntime: "docker"}}}}
}

func TestRegistryPreflightIsMetadataOnlyAndDoesNotMutateInputs(t *testing.T) {
	for _, kind := range []string{"app", "pipeline"} {
		t.Run(kind, func(t *testing.T) {
			st, s, f, r := registryAuthorityFixture(t)
			f.providerErr = errors.New(registryCanary)
			registries := &preflightRegistryStore{RegistryConfigStore: st.Registries}
			envs := &preflightEnvironmentStore{EnvironmentStore: st.Environments}
			s.Registries, s.Environments = registries, envs
			app, p := preflightApp(r.ID), preflightPipeline(r.ID)
			before, _ := json.Marshal([]any{app, p})
			var err error
			if kind == "app" {
				err = CheckAppRegistryPreflight(context.Background(), s, app)
			} else {
				err = CheckPipelineRegistryPreflight(context.Background(), s, p)
			}
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal([]any{app, p})
			if string(before) != string(after) {
				t.Fatal("preflight mutated authored selection")
			}
			if registries.reads != 1 || envs.reads != 0 || f.pins+f.reads+f.latestReads != 0 {
				t.Fatal("preflight exceeded metadata boundary", registries.reads, envs.reads, f)
			}
		})
	}
}

func TestRegistryPreflightRejectsUnsafeMetadataWithoutCredentialAccess(t *testing.T) {
	cases := []struct {
		name   string
		change func(*model.RegistryConfig) (*model.RegistryConfig, error)
	}{
		{"ungranted", func(r *model.RegistryConfig) (*model.RegistryConfig, error) {
			r.AllowedAppIDs = nil
			r.AllowedPipelineIDs = nil
			return r, nil
		}},
		{"wrong identity", func(r *model.RegistryConfig) (*model.RegistryConfig, error) { r.ID = "another"; return r, nil }},
		{"version absent", func(r *model.RegistryConfig) (*model.RegistryConfig, error) { r.Version = 0; return r, nil }},
		{"unsafe endpoint", func(r *model.RegistryConfig) (*model.RegistryConfig, error) {
			r.URL = "https://user:" + registryCanary + "@ghcr.io"
			return r, nil
		}},
		{"endpoint scheme", func(r *model.RegistryConfig) (*model.RegistryConfig, error) {
			r.URL = "file:///tmp/private"
			return r, nil
		}},
		{"unsafe username", func(r *model.RegistryConfig) (*model.RegistryConfig, error) {
			r.Username = "username\n" + registryCanary
			return r, nil
		}},
		{"missing", func(*model.RegistryConfig) (*model.RegistryConfig, error) { return nil, nil }},
		{"store error canary", func(*model.RegistryConfig) (*model.RegistryConfig, error) { return nil, errors.New(registryCanary) }},
	}
	for _, kind := range []string{"app", "pipeline"} {
		for _, tc := range cases {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				st, s, f, r := registryAuthorityFixture(t)
				s.Registries = &preflightRegistryStore{RegistryConfigStore: st.Registries, change: tc.change}
				envs := &preflightEnvironmentStore{EnvironmentStore: st.Environments}
				s.Environments = envs
				var err error
				if kind == "app" {
					err = CheckAppRegistryPreflight(context.Background(), s, preflightApp(r.ID))
				} else {
					err = CheckPipelineRegistryPreflight(context.Background(), s, preflightPipeline(r.ID))
				}
				if err == nil || strings.Contains(err.Error(), registryCanary) {
					t.Fatal("unsafe metadata accepted or leaked", err)
				}
				if envs.reads != 0 || f.pins+f.reads+f.latestReads != 0 {
					t.Fatal("rejection read credentials")
				}
			})
		}
	}
}

func TestRegistryPreflightSelectionAndTransportFailBeforeMetadata(t *testing.T) {
	for _, kind := range []string{"app", "pipeline"} {
		for _, name := range []string{"duplicate selection", "invalid selection", "imported target", "unsupported transport"} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				st, s, f, _ := registryAuthorityFixture(t)
				registries := &preflightRegistryStore{RegistryConfigStore: st.Registries}
				s.Registries = registries
				app, p := preflightApp("registry"), preflightPipeline("registry")
				switch name {
				case "duplicate selection":
					app.BuildPlan.RegistryConnectionIDs = []string{"registry", "registry"}
					p.Stages[0].Config.RegistryConnectionIDs = []string{"registry", "registry"}
				case "invalid selection":
					app.BuildPlan.RegistryConnectionIDs = []string{"../registry"}
					p.Stages[0].Config.RegistryConnectionIDs = []string{"../registry"}
				case "imported target":
					app.ID = "vapp_imported"
					p.ID = "vflow_imported"
				case "unsupported transport":
					app.DeployTarget.Kind = model.DeployTargetKind("kubernetes")
					p.Stages[1].Config.DeployRuntime = "kubernetes"
				}
				var err error
				if kind == "app" {
					err = CheckAppRegistryPreflight(context.Background(), s, app)
				} else {
					err = CheckPipelineRegistryPreflight(context.Background(), s, p)
				}
				if err == nil || registries.reads != 0 || f.pins+f.reads+f.latestReads != 0 {
					t.Fatal("invalid input crossed metadata boundary", err, registries.reads)
				}
			})
		}
	}
}

func TestRegistryPreflightDuplicateHostMatchesStageBoundary(t *testing.T) {
	st, s, f, r := registryAuthorityFixture(t)
	second := r.Clone()
	second.ID = "second"
	second.Name = "second"
	second.URL = "https://ghcr.io/other"
	if err := st.Registries.Create(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if CheckAppRegistryPreflight(context.Background(), s, preflightApp(r.ID, second.ID)) == nil {
		t.Fatal("App accepted ambiguous host credentials")
	}
	p := preflightPipeline(r.ID, second.ID)
	if CheckPipelineRegistryPreflight(context.Background(), s, p) == nil {
		t.Fatal("stage accepted ambiguous host credentials")
	}
	p.Stages[0].Config.RegistryConnectionIDs = []string{r.ID}
	p.Stages[1].Config.RegistryConnectionIDs = []string{second.ID}
	if err := CheckPipelineRegistryPreflight(context.Background(), s, p); err != nil {
		t.Fatal("independent stages rejected", err)
	}
	if f.pins+f.reads+f.latestReads != 0 {
		t.Fatal("host validation pinned credentials")
	}
}

func TestRegistryPreflightAnonymousAndOwnerKindRemainExplicit(t *testing.T) {
	if err := CheckAppRegistryPreflight(context.Background(), nil, preflightApp()); err != nil {
		t.Fatal(err)
	}
	if err := CheckPipelineRegistryPreflight(context.Background(), nil, preflightPipeline()); err != nil {
		t.Fatal(err)
	}
	_, s, _, r := registryAuthorityFixture(t)
	app := preflightApp(r.ID)
	app.ID = "pipe-a"
	p := preflightPipeline(r.ID)
	p.ID = "app-a"
	if !errors.Is(CheckAppRegistryPreflight(context.Background(), s, app), ErrRegistryAuthorityDenied) || !errors.Is(CheckPipelineRegistryPreflight(context.Background(), s, p), ErrRegistryAuthorityDenied) {
		t.Fatal("grants crossed owner kinds")
	}
	if !errors.Is(CheckAppRegistryPreflight(context.Background(), nil, preflightApp(r.ID)), ErrRegistryAuthorityRequired) || !errors.Is(CheckPipelineRegistryPreflight(context.Background(), nil, preflightPipeline(r.ID)), ErrRegistryAuthorityRequired) {
		t.Fatal("selected credentials accepted without authority")
	}
}
