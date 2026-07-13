package service

import (
	"context"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

// fakeSecrets is a minimal secrets.Manager for resolver tests.
type fakeSecrets struct {
	data map[string]map[string][]byte // envID -> key -> value
}

func (f *fakeSecrets) Get(_ context.Context, envID, key string) ([]byte, error) {
	return f.data[envID][key], nil
}
func (f *fakeSecrets) Put(_ context.Context, envID, key string, value []byte) error {
	if f.data[envID] == nil {
		f.data[envID] = map[string][]byte{}
	}
	f.data[envID][key] = value
	return nil
}
func (f *fakeSecrets) Delete(_ context.Context, envID, key string) error {
	delete(f.data[envID], key)
	return nil
}
func (f *fakeSecrets) List(_ context.Context, envID string) ([]string, error) {
	keys := make([]string, 0, len(f.data[envID]))
	for k := range f.data[envID] {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestAppEnvResolver_MergePrecedence(t *testing.T) {
	st := memory.New()
	env := &model.Environment{
		ID:        "env-1",
		Name:      "staging",
		PlainVars: map[string]string{"SHARED": "plain", "PLAIN_ONLY": "p"},
	}
	if err := st.Environments.Create(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	r := &AppEnvResolver{
		Environments: st.Environments,
		Secrets: &fakeSecrets{data: map[string]map[string][]byte{
			"env-1": {"SHARED": []byte("secret"), "SECRET_ONLY": []byte("s")},
		}},
	}
	app := &model.App{ID: "a1", EnvironmentID: "env-1"}
	got, err := r.Resolve(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	// Secrets override PlainVars on collision.
	if got["SHARED"] != "secret" {
		t.Errorf("SHARED = %q, want secret to win over plain", got["SHARED"])
	}
	if got["PLAIN_ONLY"] != "p" || got["SECRET_ONLY"] != "s" {
		t.Errorf("missing keys: %v", got)
	}
	// Stage-explicit env wins over everything via mergeEnv.
	final := mergeEnv(got, map[string]string{"SHARED": "stage"})
	if final["SHARED"] != "stage" {
		t.Errorf("stage override lost: %v", final)
	}
}

func TestAppEnvResolver_NilSafety(t *testing.T) {
	var r *AppEnvResolver
	got, err := r.Resolve(context.Background(), &model.App{ID: "a"})
	if err != nil || len(got) != 0 {
		t.Fatalf("nil resolver must return empty map: %v %v", got, err)
	}
	r2 := &AppEnvResolver{}
	got, err = r2.Resolve(context.Background(), &model.App{ID: "a", EnvironmentID: ""})
	if err != nil || len(got) != 0 {
		t.Fatalf("unlinked app must return empty map: %v %v", got, err)
	}
}

func TestSynthesizePipeline_InjectsEnvAndIngress(t *testing.T) {
	app := &model.App{
		ID: "a1", Name: "My App",
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes, Namespace: "default"},
	}
	opts := synthOpts{
		// Awkward value: quotes, colon, newline — must survive YAML-safe
		// rendering without breaking the manifest.
		appEnv: map[string]string{"API_KEY": "va\"l: ue\nx", "B": "2"},
		proxy:  ProxyConfig{Domain: "apps.example.com", Scheme: "https", IngressClass: "nginx"},
	}
	p, _ := synthesizePipeline(app, nil, "/tmp/wd", "reg/my-app:1", nil, opts)

	var deploy *model.Stage
	for i := range p.Stages {
		if p.Stages[i].Type == model.StageTypeDeploy {
			deploy = &p.Stages[i]
		}
	}
	if deploy == nil {
		t.Fatal("no deploy stage synthesized")
	}
	if deploy.Config.ProxyHost != "my-app.apps.example.com" {
		t.Errorf("ProxyHost = %q", deploy.Config.ProxyHost)
	}
	if deploy.Config.Env["API_KEY"] == "" {
		t.Errorf("stage env not stamped: %v", deploy.Config.Env)
	}
	m := deploy.Config.ManifestPath
	if !strings.Contains(m, "env:") || !strings.Contains(m, "name: API_KEY") {
		t.Errorf("manifest missing env block:\n%s", m)
	}
	// yaml-safe quoting of the awkward value.
	if !strings.Contains(m, "va") || strings.Contains(m, "\tva") {
		t.Errorf("env value mangled:\n%s", m)
	}
	if !strings.Contains(m, "kind: Ingress") || !strings.Contains(m, "host: my-app.apps.example.com") {
		t.Errorf("manifest missing Ingress:\n%s", m)
	}
	if !strings.Contains(m, "ingressClassName: nginx") {
		t.Errorf("manifest missing ingressClassName:\n%s", m)
	}
}

func TestSynthesizePipeline_NoProxyNoIngress(t *testing.T) {
	app := &model.App{
		ID: "a1", Name: "app",
		DeployTarget: model.DeployTarget{Kind: model.DeployTargetKubernetes},
	}
	p, _ := synthesizePipeline(app, nil, "/tmp/wd", "reg/app:1", nil, synthOpts{})
	for i := range p.Stages {
		if p.Stages[i].Type != model.StageTypeDeploy {
			continue
		}
		if strings.Contains(p.Stages[i].Config.ManifestPath, "kind: Ingress") {
			t.Error("Ingress synthesized without a proxy domain")
		}
		if p.Stages[i].Config.ProxyHost != "" {
			t.Error("ProxyHost stamped without a proxy domain")
		}
	}
}

func TestDeployedURLFor(t *testing.T) {
	cases := []struct {
		name  string
		p     *model.Pipeline
		proxy ProxyConfig
		want  string
	}{
		{"nil pipeline", nil, ProxyConfig{}, ""},
		{"proxy host wins", &model.Pipeline{Stages: []model.Stage{{
			Type: model.StageTypeDeploy, Config: model.StageConfig{ProxyHost: "a.apps.io"},
		}}}, ProxyConfig{Scheme: "https"}, "https://a.apps.io"},
		{"docker ports fallback", &model.Pipeline{Stages: []model.Stage{{
			Type: model.StageTypeDeploy, Config: model.StageConfig{DeployRuntime: "docker", ComposePorts: []string{"8081:80"}},
		}}}, ProxyConfig{}, "http://localhost:8081"},
		{"no signal", &model.Pipeline{Stages: []model.Stage{{
			Type: model.StageTypeDeploy, Config: model.StageConfig{},
		}}}, ProxyConfig{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deployedURLFor(tc.p, tc.proxy); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}
