package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/build/registryauth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/store"
)

var (
	ErrRegistryAuthorityRequired     = errors.New("registry: accepted authority is required")
	ErrRegistryAuthorityDenied       = errors.New("registry: connection is not authorized")
	ErrRegistryAuthorityChanged      = errors.New("registry: accepted authority has changed")
	ErrRegistryCredentialUnavailable = errors.New("registry: pinned credential is unavailable")
	ErrRegistryLegacyExternal        = errors.New("registry: replace legacy external credential using portable scope")
)

type RegistryAuthorityService struct {
	Registries   store.RegistryConfigStore
	Environments store.EnvironmentStore
	Secrets      secrets.Manager
}

func NewRegistryAuthorityService(st *store.Store, manager secrets.Manager) *RegistryAuthorityService {
	s := &RegistryAuthorityService{Secrets: manager}
	if st != nil {
		s.Registries = st.Registries
		s.Environments = st.Environments
	}
	return s
}

// RegistryCredentialScope exposes the nonsecret protocol for operators/tests.
// A v2 incarnation is unrelated to names and is never reused during replacement.
func RegistryCredentialScope(ref, connectionID string) (envID, key string, legacy bool, err error) {
	if strings.HasPrefix(ref, "registry:v2:") {
		id := strings.TrimPrefix(ref, "registry:v2:")
		u, e := uuid.Parse(id)
		if e != nil || u == uuid.Nil || u.String() != id {
			return "", "", false, model.ErrInvalidRegistryAuthority
		}
		return "registry_" + strings.ReplaceAll(id, "-", ""), "password", false, nil
	}
	if ref == registryKeyRef(connectionID) && secrets.ValidSegment(connectionID) {
		return registrySecretsEnvID, registryPasswordKey + "." + connectionID, true, nil
	}
	return "", "", false, model.ErrInvalidRegistryAuthority
}

// IsRegistryCredentialEnvironmentID protects private Settings scopes from
// runtime Environment bindings. Reserve the prefix, including malformed forms.
func IsRegistryCredentialEnvironmentID(id string) bool {
	return model.IsRegistryCredentialEnvironmentID(id)
}

// IsRegistryCredentialEnvironment also protects friendly-name aliases. KeepSave
// resolves by Environment.Name, so a public UUID with the same private name
// must not be permitted to read/inject registry credentials as runtime secrets.
func IsRegistryCredentialEnvironment(env *model.Environment) bool {
	return model.IsRegistryCredentialEnvironment(env)
}

func granted(r *model.RegistryConfig, subject model.RegistrySubject) bool {
	ids := r.AllowedAppIDs
	if subject.Kind == "pipeline" {
		ids = r.AllowedPipelineIDs
	}
	for _, id := range ids {
		if id == subject.ID {
			return true
		}
	}
	return false
}
func registryPin(r *model.RegistryConfig) (model.RegistryConnectionPin, error) {
	endpoint, host, err := registryauth.Endpoint(r.URL)
	if err != nil {
		return model.RegistryConnectionPin{}, model.ErrInvalidRegistryAuthority
	}
	return model.RegistryConnectionPin{ID: r.ID, Version: r.Version, Endpoint: endpoint, Host: host, Username: r.Username, Credential: model.RegistryCredentialPin{Mode: "none"}}, nil
}

// Prepare checks all requested grants before any credential/provider read. The
// union must match authored stage selections exactly, preventing extra pins.
func (s *RegistryAuthorityService) Prepare(ctx context.Context, subject model.RegistrySubject, selectedIDs []string, stageBindings map[string][]string) (*model.RegistryAuthorityPlan, error) {
	if subject.Validate() != nil || !model.ValidRegistryIDs(selectedIDs, 32) || len(stageBindings) > 256 {
		return nil, model.ErrInvalidRegistryAuthority
	}
	plan := &model.RegistryAuthorityPlan{Version: 1, Subject: subject, Connections: map[string]model.RegistryConnectionPin{}}
	if subject.Kind == "pipeline" {
		plan.Stages = map[string][]string{}
		union := map[string]bool{}
		for stage, ids := range stageBindings {
			if !secrets.ValidSegment(stage) || !model.ValidRegistryIDs(ids, 32) {
				return nil, model.ErrInvalidRegistryAuthority
			}
			plan.Stages[stage] = append([]string(nil), ids...)
			for _, id := range ids {
				union[id] = true
			}
		}
		if len(union) != len(selectedIDs) {
			return nil, model.ErrInvalidRegistryAuthority
		}
		for _, id := range selectedIDs {
			if !union[id] {
				return nil, model.ErrInvalidRegistryAuthority
			}
		}
	} else if len(stageBindings) != 0 {
		return nil, model.ErrInvalidRegistryAuthority
	}
	configs := map[string]*model.RegistryConfig{}
	for _, id := range selectedIDs {
		if s == nil || s.Registries == nil {
			return nil, ErrRegistryAuthorityDenied
		}
		r, err := s.Registries.Get(ctx, id)
		if err != nil || r == nil || r.Version < 1 || !granted(r, subject) {
			return nil, ErrRegistryAuthorityDenied
		}
		pin, err := registryPin(r)
		if err != nil {
			return nil, err
		}
		plan.Connections[id] = pin
		configs[id] = r
	}
	if err := checkRegistryHosts(plan); err != nil {
		return nil, err
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	for _, id := range selectedIDs {
		pin := plan.Connections[id]
		credential, err := s.pinCredential(ctx, configs[id])
		if err != nil {
			return nil, err
		}
		pin.Credential = credential
		plan.Connections[id] = pin
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if err := s.ValidateCurrent(ctx, subject, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func checkRegistryHosts(plan *model.RegistryAuthorityPlan) error {
	check := func(ids []string) error {
		seen := map[string]bool{}
		for _, id := range ids {
			h := plan.Connections[id].Host
			if seen[h] {
				return model.ErrInvalidRegistryAuthority
			}
			seen[h] = true
		}
		return nil
	}
	if plan.Subject.Kind == "pipeline" {
		for _, ids := range plan.Stages {
			if err := check(ids); err != nil {
				return err
			}
		}
		return nil
	}
	ids := make([]string, 0, len(plan.Connections))
	for id := range plan.Connections {
		ids = append(ids, id)
	}
	return check(ids)
}

func (s *RegistryAuthorityService) pinCredential(ctx context.Context, r *model.RegistryConfig) (model.RegistryCredentialPin, error) {
	if r.PasswordRef == "" {
		return model.RegistryCredentialPin{Mode: "none"}, nil
	}
	envID, key, legacy, err := RegistryCredentialScope(r.PasswordRef, r.ID)
	if err != nil {
		return model.RegistryCredentialPin{}, err
	}
	if generationBoundManager(s.Secrets) {
		if s.Environments == nil {
			return model.RegistryCredentialPin{}, ErrRegistryCredentialUnavailable
		}
		env, e := s.Environments.Get(ctx, envID)
		if e != nil || env.Version < 1 || env.CreatedAt.IsZero() {
			return model.RegistryCredentialPin{}, ErrRegistryCredentialUnavailable
		}
		if _, ok := env.Secrets[key]; !ok {
			return model.RegistryCredentialPin{}, ErrRegistryCredentialUnavailable
		}
		return model.RegistryCredentialPin{Mode: "database", EnvironmentID: envID, Key: key, EnvironmentVersion: env.Version, EnvironmentCreatedAt: env.CreatedAt}, nil
	}
	if legacy {
		return model.RegistryCredentialPin{}, ErrRegistryLegacyExternal
	}
	ref, e := secrets.PinVersion(ctx, s.Secrets, envID, key)
	if e != nil || ref.Validate() != nil || ref.Scope.EnvironmentID != envID || ref.Scope.Key != key {
		return model.RegistryCredentialPin{}, ErrRegistryCredentialUnavailable
	}
	return model.RegistryCredentialPin{Mode: "external", EnvironmentID: envID, Key: key, Reference: &ref}, nil
}

func (s *RegistryAuthorityService) current(ctx context.Context, subject model.RegistrySubject, pin model.RegistryConnectionPin) error {
	if s == nil || s.Registries == nil {
		return ErrRegistryAuthorityChanged
	}
	r, err := s.Registries.Get(ctx, pin.ID)
	if err != nil || r == nil || r.Version != pin.Version || !granted(r, subject) {
		return ErrRegistryAuthorityChanged
	}
	now, err := registryPin(r)
	if err != nil || now.Endpoint != pin.Endpoint || now.Host != pin.Host || now.Username != pin.Username {
		return ErrRegistryAuthorityChanged
	}
	if r.PasswordRef == "" {
		if pin.Credential.Mode != "none" {
			return ErrRegistryAuthorityChanged
		}
		return nil
	}
	envID, key, legacy, err := RegistryCredentialScope(r.PasswordRef, r.ID)
	if err != nil || envID != pin.Credential.EnvironmentID || key != pin.Credential.Key {
		return ErrRegistryAuthorityChanged
	}
	if legacy && !generationBoundManager(s.Secrets) {
		return ErrRegistryLegacyExternal
	}
	switch pin.Credential.Mode {
	case "database":
		if !generationBoundManager(s.Secrets) || s.Environments == nil {
			return ErrRegistryCredentialUnavailable
		}
		env, e := s.Environments.Get(ctx, envID)
		if e != nil || env.Version != pin.Credential.EnvironmentVersion || !env.CreatedAt.Equal(pin.Credential.EnvironmentCreatedAt) {
			return ErrRegistryAuthorityChanged
		}
		if _, ok := env.Secrets[key]; !ok {
			return ErrRegistryCredentialUnavailable
		}
	case "external":
		if generationBoundManager(s.Secrets) {
			return ErrRegistryCredentialUnavailable
		}
		if _, ok := s.Secrets.(secrets.VersionedManager); !ok {
			return ErrRegistryCredentialUnavailable
		}
	default:
		return ErrRegistryAuthorityChanged
	}
	return nil
}

func (s *RegistryAuthorityService) ValidateCurrent(ctx context.Context, subject model.RegistrySubject, plan *model.RegistryAuthorityPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if plan.Subject != subject || checkRegistryHosts(plan) != nil {
		return model.ErrInvalidRegistryAuthority
	}
	for _, pin := range plan.Connections {
		if err := s.current(ctx, subject, pin); err != nil {
			return err
		}
	}
	return nil
}

// RegistryAuthorityHosts returns the exact host set for an accepted scope. It
// is used for private Docker auth files, never global registry discovery.
func RegistryAuthorityHosts(plan *model.RegistryAuthorityPlan, stageID string) ([]string, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(plan.Connections))
	if plan.Subject.Kind == "pipeline" {
		var ok bool
		ids, ok = plan.Stages[stageID]
		if !ok {
			return nil, model.ErrInvalidRegistryAuthority
		}
	} else {
		for id := range plan.Connections {
			ids = append(ids, id)
		}
	}
	hosts := make([]string, 0, len(ids))
	for _, id := range ids {
		hosts = append(hosts, plan.Connections[id].Host)
	}
	sort.Strings(hosts)
	return hosts, nil
}

// Resolver clones evidence before returning the callback, so callers cannot
// expand its selection by editing a retained plan pointer. It never lists
// global configs or reads latest external values.
func (s *RegistryAuthorityService) Resolver(ctx context.Context, subject model.RegistrySubject, plan *model.RegistryAuthorityPlan, stageID string) (registryauth.Resolver, error) {
	if err := s.ValidateCurrent(ctx, subject, plan); err != nil {
		return nil, err
	}
	hosts, err := RegistryAuthorityHosts(plan, stageID)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(plan)
	var snapshot model.RegistryAuthorityPlan
	if json.Unmarshal(encoded, &snapshot) != nil {
		return nil, model.ErrInvalidRegistryAuthority
	}
	allowed := map[string]bool{}
	for _, h := range hosts {
		allowed[h] = true
	}
	return func(readCtx context.Context, rawHost string) (authn.Authenticator, error) {
		_, host, e := registryauth.Endpoint(rawHost)
		if e != nil {
			return nil, model.ErrInvalidRegistryAuthority
		}
		if !allowed[host] {
			return authn.Anonymous, nil
		}
		for _, pin := range snapshot.Connections {
			if pin.Host != host {
				continue
			}
			// A Pipeline may select different same-host connections in different
			// stages. Match this stage's ID as well as its host.
			if subject.Kind == "pipeline" {
				found := false
				for _, id := range snapshot.Stages[stageID] {
					if id == pin.ID {
						found = true
					}
				}
				if !found {
					continue
				}
			}
			if e := s.current(readCtx, subject, pin); e != nil {
				return nil, e
			}
			if pin.Credential.Mode == "none" {
				return authn.Anonymous, nil
			}
			var value []byte
			if pin.Credential.Mode == "database" {
				value, e = s.Secrets.Get(readCtx, pin.Credential.EnvironmentID, pin.Credential.Key)
			} else {
				value, e = secrets.ReadPinned(readCtx, s.Secrets, pin.Credential.EnvironmentID, pin.Credential.Key, *pin.Credential.Reference)
			}
			if e != nil || len(value) > 65536 {
				return nil, ErrRegistryCredentialUnavailable
			}
			if e := s.current(readCtx, subject, pin); e != nil {
				return nil, e
			}
			password := string(value)
			basic := pin.Username + ":" + password
			forms := []string{password, basic, base64.StdEncoding.EncodeToString([]byte(basic)), base64.RawStdEncoding.EncodeToString([]byte(basic)), url.QueryEscape(password)}
			RegisterLogSecrets(ctx, forms...)
			RegisterLogSecrets(readCtx, forms...)
			return authn.FromConfig(authn.AuthConfig{Username: pin.Username, Password: string(value)}), nil
		}
		return nil, model.ErrInvalidRegistryAuthority
	}, nil
}
