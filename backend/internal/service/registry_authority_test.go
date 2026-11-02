package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/store"
	"github.com/santapong/cooker/internal/store/memory"
)

const registryCanary = "registry-owned-secret-canary-SECRET-123"

type registryVersionFixture struct {
	latest                   string
	values                   map[string]string
	pins, reads, latestReads int
	providerErr              error
	onPin, onRead            func()
	badScope                 bool
}

func (f *registryVersionFixture) Get(context.Context, string, string) ([]byte, error) {
	f.latestReads++
	return nil, errors.New(registryCanary)
}
func (f *registryVersionFixture) Put(context.Context, string, string, []byte) error {
	return f.providerErr
}
func (f *registryVersionFixture) Delete(context.Context, string, string) error { return nil }
func (f *registryVersionFixture) List(context.Context, string) ([]string, error) {
	return nil, errors.New(registryCanary)
}
func (f *registryVersionFixture) Pin(_ context.Context, id, key string) (secrets.VersionReference, error) {
	f.pins++
	if f.onPin != nil {
		f.onPin()
	}
	if f.providerErr != nil {
		return secrets.VersionReference{}, f.providerErr
	}
	if f.badScope {
		id = "substituted"
	}
	return secrets.VersionReference{SchemaVersion: 1, Provider: "vault", Resource: "kv/data/" + id + "/" + key, Version: f.latest, Incarnation: "2026-10-02T12:00:00Z", Scope: secrets.VersionScope{Authority: "https://owned-vault.invalid", Mount: "kv", EnvironmentID: id, Key: key}}, nil
}
func (f *registryVersionFixture) GetPinned(_ context.Context, id, key string, ref secrets.VersionReference) ([]byte, error) {
	f.reads++
	if f.onRead != nil {
		f.onRead()
	}
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	if ref.Scope.EnvironmentID != id || ref.Scope.Key != key {
		return nil, secrets.ErrVersionMismatch
	}
	v, ok := f.values[ref.Version]
	if !ok {
		return nil, secrets.ErrVersionUnavailable
	}
	return []byte(v), nil
}
func registryAuthorityFixture(t *testing.T) (*store.Store, *RegistryAuthorityService, *registryVersionFixture, *model.RegistryConfig) {
	t.Helper()
	st := memory.New()
	f := &registryVersionFixture{latest: "1", values: map[string]string{"1": registryCanary, "2": "rotated-value"}}
	r := &model.RegistryConfig{ID: "registry", Name: "registry", URL: "https://ghcr.io/team", Username: "ci", PasswordRef: "registry:v2:6a2fc7f1-e326-40f5-ab7d-0b5f9400d663", AllowedAppIDs: []string{"app-a"}, AllowedPipelineIDs: []string{"pipe-a"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.Registries.Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	return st, NewRegistryAuthorityService(st, f), f, r
}

func TestRegistryAuthorityUnrelatedGrantAndSelectionReadNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		subject model.RegistrySubject
		ids     []string
	}{
		"ungranted-app":    {model.RegistrySubject{Kind: "app", ID: "app-b"}, []string{"registry"}},
		"wrong-owner-kind": {model.RegistrySubject{Kind: "pipeline", ID: "app-a"}, []string{"registry"}},
		"guessed-id":       {model.RegistrySubject{Kind: "app", ID: "app-a"}, []string{"missing"}},
		"duplicate-id":     {model.RegistrySubject{Kind: "app", ID: "app-a"}, []string{"registry", "registry"}},
		"invalid-id":       {model.RegistrySubject{Kind: "app", ID: "app-a"}, []string{"../registry"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, s, f, _ := registryAuthorityFixture(t)
			var stages map[string][]string
			if tc.subject.Kind == "pipeline" {
				stages = map[string][]string{"build": tc.ids}
			}
			if _, err := s.Prepare(context.Background(), tc.subject, tc.ids, stages); err == nil {
				t.Fatal("unrelated authority accepted")
			}
			if f.pins != 0 || f.reads != 0 || f.latestReads != 0 {
				t.Fatal("unauthorized selection read secrets")
			}
		})
	}
}

func TestRegistryAuthorityPinsSurviveJSONRotationAndFreshResolver(t *testing.T) {
	st, s, f, r := registryAuthorityFixture(t)
	ctx := WithLogSecrets(context.Background(), nil)
	subject := model.RegistrySubject{Kind: "app", ID: "app-a"}
	plan, err := s.Prepare(ctx, subject, []string{r.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(plan)
	if strings.Contains(string(data), registryCanary) {
		t.Fatal("plaintext persisted in plan")
	}
	f.latest = "2"
	var restored model.RegistryAuthorityPlan
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	resolve, err := NewRegistryAuthorityService(st, f).Resolver(ctx, subject, &restored, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := resolve(ctx, "ghcr.io")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Authorization()
	if cfg.Username != "ci" || cfg.Password != registryCanary || f.latestReads != 0 || f.reads != 1 {
		t.Fatal("exact version was not restored")
	}
	if strings.Contains(RedactLogText(ctx, registryCanary), registryCanary) {
		t.Fatal("credential not registered for redaction")
	}
	encodedBasic := base64.StdEncoding.EncodeToString([]byte("ci:" + registryCanary))
	if strings.Contains(RedactLogText(ctx, "Authorization: Basic "+encodedBasic), encodedBasic) {
		t.Fatal("basic auth form not redacted")
	}
	a, err = resolve(ctx, "unrelated.example")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ = a.Authorization()
	if cfg.Password != "" || f.reads != 1 {
		t.Fatal("unselected host borrowed credential")
	}
	delete(f.values, "1")
	if _, err = resolve(ctx, "ghcr.io"); !errors.Is(err, ErrRegistryCredentialUnavailable) {
		t.Fatal("deleted history fell back")
	}
	if f.latestReads != 0 {
		t.Fatal("latest lookup after history deletion")
	}
}

func TestRegistryAuthorityRevocationDuringPinAndReadFailsClosed(t *testing.T) {
	for _, when := range []string{"pin", "read"} {
		t.Run(when, func(t *testing.T) {
			st, s, f, r := registryAuthorityFixture(t)
			ctx := context.Background()
			subject := model.RegistrySubject{Kind: "app", ID: "app-a"}
			revoke := func() {
				if _, err := st.Registries.(store.RegistryGrantStore).UpdateGrants(ctx, r.ID, r.Version, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			if when == "pin" {
				f.onPin = revoke
				if _, err := s.Prepare(ctx, subject, []string{r.ID}, nil); !errors.Is(err, ErrRegistryAuthorityChanged) {
					t.Fatal("pin raced past revocation")
				}
				if f.reads != 0 {
					t.Fatal("plaintext read during admission")
				}
				return
			}
			plan, err := s.Prepare(ctx, subject, []string{r.ID}, nil)
			if err != nil {
				t.Fatal(err)
			}
			resolve, err := s.Resolver(ctx, subject, plan, "")
			if err != nil {
				t.Fatal(err)
			}
			f.onRead = revoke
			if _, err = resolve(ctx, "ghcr.io"); !errors.Is(err, ErrRegistryAuthorityChanged) {
				t.Fatal("plaintext released after revocation")
			}
		})
	}
}

func TestRegistryAuthorityDeletionRecreationAndScopeTamper(t *testing.T) {
	st, s, f, r := registryAuthorityFixture(t)
	ctx := context.Background()
	subject := model.RegistrySubject{Kind: "app", ID: "app-a"}
	plan, err := s.Prepare(ctx, subject, []string{r.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"subject", "username", "endpoint", "credential-scope"} {
		t.Run(field, func(t *testing.T) {
			data, _ := json.Marshal(plan)
			var p model.RegistryAuthorityPlan
			_ = json.Unmarshal(data, &p)
			pin := p.Connections[r.ID]
			switch field {
			case "subject":
				p.Subject.ID = "app-b"
			case "username":
				pin.Username = "other"
			case "endpoint":
				pin.Endpoint = "https://different.example"
			case "credential-scope":
				pin.Credential.EnvironmentID = "other"
			}
			p.Connections[r.ID] = pin
			if _, err := s.Resolver(ctx, subject, &p, ""); err == nil {
				t.Fatal("substituted authority accepted")
			}
		})
	}
	if f.reads != 0 {
		t.Fatal("tamper read plaintext")
	}
	if err = st.Registries.Delete(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	recreated := r.Clone()
	if err = st.Registries.Create(ctx, recreated); err != nil {
		t.Fatal(err)
	}
	if recreated.Version == r.Version {
		t.Fatal("generation reused")
	}
	if _, err = s.Resolver(ctx, subject, plan, ""); !errors.Is(err, ErrRegistryAuthorityChanged) {
		t.Fatal("recreated connection reused old plan")
	}
}

func TestRegistryAuthorityEmptyPlanAndMissingEvidence(t *testing.T) {
	_, s, f, _ := registryAuthorityFixture(t)
	ctx := context.Background()
	subject := model.RegistrySubject{Kind: "app", ID: "app-a"}
	plan, err := s.Prepare(ctx, subject, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := s.Resolver(ctx, subject, plan, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := resolve(ctx, "ghcr.io")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Authorization()
	if cfg.Password != "" || f.pins != 0 || f.reads != 0 {
		t.Fatal("empty plan discovered credential")
	}
	if _, err = s.Resolver(ctx, subject, nil, ""); err == nil {
		t.Fatal("absent authority accepted")
	}
}

func TestRegistryAuthorityPipelineStageBindings(t *testing.T) {
	st, s, f, r := registryAuthorityFixture(t)
	ctx := context.Background()
	subject := model.RegistrySubject{Kind: "pipeline", ID: "pipe-a"}
	r2 := r.Clone()
	r2.ID = "second"
	r2.Name = "second"
	r2.Username = "second-user"
	if err := st.Registries.Create(ctx, r2); err != nil {
		t.Fatal(err)
	}
	plan, err := s.Prepare(ctx, subject, []string{r.ID, r2.ID}, map[string][]string{"build": {r.ID}, "push": {r2.ID}, "public": {}})
	if err != nil {
		t.Fatal(err)
	}
	for stage, wantUser := range map[string]string{"build": "ci", "push": "second-user", "public": ""} {
		resolve, e := s.Resolver(ctx, subject, plan, stage)
		if e != nil {
			t.Fatal(e)
		}
		a, e := resolve(ctx, "ghcr.io")
		if e != nil {
			t.Fatal(e)
		}
		cfg, _ := a.Authorization()
		if cfg.Username != wantUser {
			t.Fatal("stage borrowed another selection")
		}
	}
	if _, err = s.Resolver(ctx, subject, plan, "unknown"); err == nil {
		t.Fatal("unknown stage accepted")
	}
	before := f.pins
	if _, err = s.Prepare(ctx, subject, []string{r.ID, r2.ID}, map[string][]string{"build": {r.ID}}); err == nil {
		t.Fatal("unbound extra pin accepted")
	}
	if _, err = s.Prepare(ctx, subject, []string{r.ID, r2.ID}, map[string][]string{"build": {r.ID, r2.ID}}); err == nil {
		t.Fatal("ambiguous same-stage host accepted")
	}
	if f.pins != before {
		t.Fatal("invalid stage selection read provider metadata")
	}
}

func TestRegistryAuthorityProviderErrorsAndMalformedMetadataStaySafe(t *testing.T) {
	for _, kind := range []string{"pin-error", "read-error", "scope"} {
		t.Run(kind, func(t *testing.T) {
			_, s, f, r := registryAuthorityFixture(t)
			ctx := context.Background()
			subject := model.RegistrySubject{Kind: "app", ID: "app-a"}
			if kind == "pin-error" {
				f.providerErr = errors.New(registryCanary)
			}
			if kind == "scope" {
				f.badScope = true
			}
			p, err := s.Prepare(ctx, subject, []string{r.ID}, nil)
			if kind == "read-error" {
				if err != nil {
					t.Fatal(err)
				}
				resolve, e := s.Resolver(ctx, subject, p, "")
				if e != nil {
					t.Fatal(e)
				}
				f.providerErr = errors.New(registryCanary)
				_, err = resolve(ctx, "ghcr.io")
			}
			if err == nil || strings.Contains(err.Error(), registryCanary) {
				t.Fatal("unsafe or absent error")
			}
		})
	}
}

func TestRegistryAuthorityDatabaseGenerationAndLegacyCompatibility(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	manager := database.New(st.Environments, codec)
	settings := NewRegistryConfigService(st, manager)
	r := &model.RegistryConfig{ID: "new-registry", Name: "new", URL: "ghcr.io", AllowedAppIDs: []string{"app"}}
	if err = settings.CreateRegistry(ctx, r, []byte(registryCanary)); err != nil {
		t.Fatal(err)
	}
	env, key, legacy, err := RegistryCredentialScope(r.PasswordRef, r.ID)
	if err != nil || legacy || !secrets.ValidSegment(env) || key != "password" {
		t.Fatal("portable scope missing")
	}
	s := NewRegistryAuthorityService(st, manager)
	subject := model.RegistrySubject{Kind: "app", ID: "app"}
	p, err := s.Prepare(ctx, subject, []string{r.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := s.Resolver(ctx, subject, p, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resolve(ctx, "ghcr.io"); err != nil {
		t.Fatal(err)
	}
	if err = manager.Put(ctx, env, key, []byte("rotation")); err != nil {
		t.Fatal(err)
	}
	if _, err = resolve(ctx, "ghcr.io"); !errors.Is(err, ErrRegistryAuthorityChanged) {
		t.Fatal("database latest credential reused")
	}
	legacyRow := &model.RegistryConfig{ID: "legacy", Name: "legacy", URL: "legacy.example", PasswordRef: registryKeyRef("legacy"), AllowedAppIDs: []string{"app"}}
	if err = st.Environments.Create(ctx, &model.Environment{ID: registrySecretsEnvID, Name: "legacy", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err = manager.Put(ctx, registrySecretsEnvID, registryPasswordKey+".legacy", []byte(registryCanary)); err != nil {
		t.Fatal(err)
	}
	if err = st.Registries.Create(ctx, legacyRow); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Prepare(ctx, subject, []string{legacyRow.ID}, nil); err != nil {
		t.Fatal("legacy generation-bound credential was not preserved", err)
	}
	external := &registryVersionFixture{}
	if _, err = NewRegistryAuthorityService(st, external).Prepare(ctx, subject, []string{legacyRow.ID}, nil); !errors.Is(err, ErrRegistryLegacyExternal) || external.pins != 0 || external.latestReads != 0 {
		t.Fatal("legacy external naming fell back")
	}
}

func TestRegistryCredentialEnvironmentAliasesRemainPrivate(t *testing.T) {
	for _, env := range []*model.Environment{
		{ID: "_registries", Name: "renamed"},
		{ID: "registry_6a2fc7f1e32640f5ab7d0b5f9400d663", Name: "renamed"},
		{ID: "public-uuid", Name: "registry_6a2fc7f1e32640f5ab7d0b5f9400d663"},
		{ID: "public-uuid", Name: "_registries (internal)"},
		{ID: "public-uuid", Name: "_registries"},
		{ID: "public-uuid", Name: " REGISTRY_private "},
	} {
		if !IsRegistryCredentialEnvironment(env) {
			t.Fatal("private ID/name alias became a runtime environment")
		}
	}
	if IsRegistryCredentialEnvironment(&model.Environment{ID: "public-uuid", Name: "production"}) || IsRegistryCredentialEnvironment(nil) {
		t.Fatal("ordinary environment misclassified")
	}
}
