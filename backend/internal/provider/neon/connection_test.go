package neon

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

const connectionFixtureKey = "synthetic-neon-management-key-not-real"

type connectionCodecFixture struct {
	active bool
	aead   cipher.AEAD
}

func (c *connectionCodecFixture) Active() bool { return c.active }
func (c *connectionCodecFixture) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plain, nil), nil
}
func (c *connectionCodecFixture) Open(sealed []byte) ([]byte, error) {
	if len(sealed) < c.aead.NonceSize() {
		return nil, errors.New("synthetic-private-codec-canary")
	}
	return c.aead.Open(nil, sealed[:c.aead.NonceSize()], sealed[c.aead.NonceSize():], nil)
}

type connectionAPIFixture struct {
	mu                                              sync.Mutex
	projectCalls, branchCalls                       int
	projectError, branchError                       error
	partialProjects, partialBranches, foreignBranch bool
	entered, release                                chan struct{}
	afterBranches                                   func()
	afterProjects                                   func()
}

func (f *connectionAPIFixture) Projects(ctx context.Context, _ string, org string) (*ProjectInventory, error) {
	f.mu.Lock()
	f.projectCalls++
	err, partial, after := f.projectError, f.partialProjects, f.afterProjects
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if after != nil {
		after()
	}
	now := time.Now().UTC()
	return &ProjectInventory{OrganizationID: org, Complete: !partial, ObservedAt: now, Projects: []Project{
		{ID: "project-one", OrganizationID: org, Name: "One", RegionID: "aws-us-east-2", CreatedAt: now, UpdatedAt: now},
		{ID: "project-two", OrganizationID: org, Name: "Two", RegionID: "aws-us-east-2", CreatedAt: now, UpdatedAt: now},
	}}, nil
}
func (f *connectionAPIFixture) Branches(ctx context.Context, _ string, project string) (*BranchInventory, error) {
	f.mu.Lock()
	f.branchCalls++
	err, partial, foreign, entered, release, after := f.branchError, f.partialBranches, f.foreignBranch, f.entered, f.release, f.afterBranches
	f.mu.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	if after != nil {
		after()
	}
	now := time.Now().UTC()
	branchProject := project
	if foreign {
		branchProject = "project-two"
	}
	return &BranchInventory{ProjectID: project, Complete: !partial, ObservedAt: now, Branches: []Branch{{ID: "br-one", ProjectID: branchProject, Name: "main", CreatedAt: now, UpdatedAt: now}}}, nil
}

func connectionCoreFixture(t *testing.T) (*ConnectionService, *connectionAPIFixture, *Connection) {
	t.Helper()
	block, err := aes.NewCipher(bytes.Repeat([]byte{31}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	api := &connectionAPIFixture{}
	s := &ConnectionService{Store: NewMemoryConnections(), Codec: &connectionCodecFixture{active: true, aead: aead}, API: api}
	c, err := s.Create(context.Background(), CreateConnectionRequest{Name: "Neon fixture", OrganizationID: "org-fixture", AccountMode: "unknown", APIKey: connectionFixtureKey, ProjectIDs: []string{"project-one"}, AcknowledgeKeyPermissions: true})
	if err != nil {
		t.Fatal(err)
	}
	return s, api, c
}

func requireConnectionCode(t *testing.T, err error, code string) {
	t.Helper()
	var safe *RequestError
	if !errors.As(err, &safe) || safe.Code != code {
		t.Fatalf("wrong connection error %v; want %s", err, code)
	}
	if strings.Contains(err.Error(), connectionFixtureKey) || strings.Contains(err.Error(), "canary") {
		t.Fatal("unsafe error")
	}
}

func TestConnectionCoreEncryptedEnvelopeAndSafePublicData(t *testing.T) {
	s, _, c := connectionCoreFixture(t)
	if !c.HasCredential || !c.CredentialMayWrite || c.AccountModeSource != "operator_declared" || c.State != "not_observed" || c.Snapshot != nil {
		t.Fatal("onboarding fabricated an observation/credential claim", c)
	}
	r, err := s.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(r.Credential, []byte(connectionFixtureKey)) {
		t.Fatal("plaintext credential stored")
	}
	plain, err := s.Codec.Open(r.Credential)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	var envelope connectionCredentialEnvelope
	if json.Unmarshal(plain, &envelope) != nil || envelope.Purpose != ConnectionCredentialPurpose || envelope.ConnectionID != c.ID || envelope.OrganizationID != c.OrganizationID || envelope.Key != connectionFixtureKey {
		t.Fatal("credential scope envelope missing")
	}
	data, err := json.Marshal(r)
	if err != nil || strings.Contains(string(data), connectionFixtureKey) || strings.Contains(string(data), `"Credential"`) || strings.Contains(string(data), `"credential"`) || strings.Contains(string(data), `"Lease"`) {
		t.Fatal("private record fields leaked", string(data), err)
	}
	other := cloneConnectionRecord(r)
	other.ID = "nconn_other"
	if _, err = s.key(other); err == nil {
		t.Fatal("ciphertext was valid under another connection identity")
	}
	other = cloneConnectionRecord(r)
	other.OrganizationID = "org-other"
	if _, err = s.key(other); err == nil {
		t.Fatal("ciphertext was valid under another organization")
	}
	s.Codec.(*connectionCodecFixture).active = false
	if _, err = s.Sync(context.Background(), c.ID); err == nil {
		t.Fatal("inactive codec accepted")
	}
}

func TestConnectionCoreFailureAndPartialCoverageRetainSuccessfulSnapshot(t *testing.T) {
	s, api, c := connectionCoreFixture(t)
	first, err := s.Sync(context.Background(), c.ID)
	if err != nil || first.State != "observed" || first.Snapshot == nil || len(first.Snapshot.Projects) != 1 || len(first.Snapshot.Branches) != 1 {
		t.Fatal("initial observation missing", first, err)
	}
	baseline := cloneConnectionSnapshot(first.Snapshot)
	api.mu.Lock()
	api.branchError = errors.New("synthetic-private-provider-canary " + connectionFixtureKey)
	api.mu.Unlock()
	failed, err := s.Sync(context.Background(), c.ID)
	if err != nil || failed.State != "stale" || failed.ErrorCode != "provider_unavailable" || !failed.LastSuccessAt.Equal(*first.LastSuccessAt) || !failed.Snapshot.ObservedAt.Equal(baseline.ObservedAt) {
		t.Fatal("failure erased/advanced success", failed, err)
	}
	data, _ := json.Marshal(failed)
	if strings.Contains(string(data), "canary") || strings.Contains(string(data), connectionFixtureKey) {
		t.Fatal("provider error leaked")
	}
	api.mu.Lock()
	api.branchError = nil
	api.partialBranches = true
	api.mu.Unlock()
	partial, err := s.Sync(context.Background(), c.ID)
	if err != nil || partial.State != "stale" || partial.ErrorCode != "invalid_response" || !partial.Snapshot.ObservedAt.Equal(baseline.ObservedAt) {
		t.Fatal("partial branch coverage replaced last success", partial, err)
	}
	api.mu.Lock()
	api.partialBranches = false
	api.foreignBranch = true
	api.mu.Unlock()
	foreign, err := s.Sync(context.Background(), c.ID)
	if err != nil || foreign.State != "stale" || foreign.ErrorCode != "scope_mismatch" || !foreign.LastSuccessAt.Equal(*first.LastSuccessAt) {
		t.Fatal("foreign branch row entered snapshot", foreign, err)
	}
}

func TestConnectionCoreVersionLeaseAndSelectionFenceInflightRefresh(t *testing.T) {
	s, api, c := connectionCoreFixture(t)
	api.mu.Lock()
	api.entered, api.release = make(chan struct{}), make(chan struct{})
	entered, release := api.entered, api.release
	api.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := s.Sync(context.Background(), c.ID); done <- err }()
	<-entered
	if _, err := s.Sync(context.Background(), c.ID); err == nil {
		t.Fatal("concurrent refresh admitted")
	} else {
		requireConnectionCode(t, err, "busy")
	}
	selected := []string{"project-two"}
	changed, err := s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: c.Version, ProjectIDs: &selected})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	requireConnectionCode(t, <-done, "conflict")
	if changed.Snapshot != nil || changed.LastSuccessAt != nil {
		t.Fatal("incompatible prior selection retained")
	}
	latest, err := s.Get(context.Background(), c.ID)
	if err != nil || latest.Snapshot != nil || len(latest.ProjectIDs) != 1 || latest.ProjectIDs[0] != "project-two" {
		t.Fatal("revoked selected snapshot committed", latest, err)
	}
}

func TestConnectionCorePauseReplaceDisconnectAndCanceledRead(t *testing.T) {
	s, api, c := connectionCoreFixture(t)
	observed, err := s.Sync(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	enabled := false
	paused, err := s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: c.Version, Enabled: &enabled})
	if err != nil || paused.State != "paused" || paused.Snapshot == nil {
		t.Fatal("pause erased last-known observation", paused, err)
	}
	api.mu.Lock()
	calls := api.projectCalls + api.branchCalls
	api.mu.Unlock()
	if _, err = s.Sync(context.Background(), c.ID); err == nil {
		t.Fatal("paused connection read provider")
	}
	api.mu.Lock()
	after := api.projectCalls + api.branchCalls
	api.mu.Unlock()
	if after != calls {
		t.Fatal("pause started another provider request")
	}
	key := "synthetic-replacement-neon-key"
	enabled = true
	replaced, err := s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: paused.Version, Enabled: &enabled, APIKey: &key, AcknowledgeKeyPermissions: true})
	if err != nil || replaced.Snapshot != nil || replaced.LastSuccessAt != nil || replaced.Version <= observed.Version {
		t.Fatal("replacement retained incompatible credential observation", replaced, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	api.mu.Lock()
	api.afterBranches = cancel
	api.mu.Unlock()
	canceled, err := s.Sync(ctx, c.ID)
	if err != nil || canceled.Snapshot != nil || canceled.LastSuccessAt != nil || canceled.ErrorCode != "canceled" || canceled.Syncing {
		t.Fatal("canceled reply published success or retained lease", canceled, err)
	}
	if err = s.Delete(context.Background(), c.ID, replaced.Version-1); err == nil {
		t.Fatal("stale disconnect accepted")
	}
	if err = s.Delete(context.Background(), c.ID, replaced.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(context.Background(), c.ID); err == nil {
		t.Fatal("disconnect retained local connection")
	}
}

func TestConnectionCorePartialOnboardingAndImmutableOrganization(t *testing.T) {
	s, api, c := connectionCoreFixture(t)
	api.mu.Lock()
	api.partialProjects = true
	api.mu.Unlock()
	_, err := s.Create(context.Background(), CreateConnectionRequest{Name: "Denied", OrganizationID: "org-fixture", AccountMode: "vercel_managed", APIKey: connectionFixtureKey, ProjectIDs: []string{}, AcknowledgeKeyPermissions: true})
	requireConnectionCode(t, err, "invalid_response")
	rows, err := s.List(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatal("partial onboarding persisted", rows, err)
	}
	r, err := s.Store.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.OrganizationID = "org-other"
	requireConnectionCode(t, s.Store.Update(context.Background(), r, r.Version), "conflict")
}

func TestConnectionCorePauseBetweenReadsPreventsNextProviderGET(t *testing.T) {
	s, api, c := connectionCoreFixture(t)
	if _, err := s.Sync(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	before := api.branchCalls
	api.afterProjects = func() {
		enabled := false
		if _, err := s.Update(context.Background(), c.ID, UpdateConnectionRequest{Version: c.Version, Enabled: &enabled}); err != nil {
			t.Fatal(err)
		}
	}
	api.mu.Unlock()
	_, err := s.Sync(context.Background(), c.ID)
	requireConnectionCode(t, err, "conflict")
	api.mu.Lock()
	after := api.branchCalls
	api.mu.Unlock()
	if after != before {
		t.Fatal("paused connection started a downstream provider read")
	}
	latest, err := s.Get(context.Background(), c.ID)
	if err != nil || latest.State != "paused" || latest.Snapshot == nil {
		t.Fatal("pause lost last-known data", latest, err)
	}
}

func TestConnectionCoreRequiresExplicitSelectionScopeAndKeyAcknowledgement(t *testing.T) {
	for _, name := range []string{"missing selection", "duplicate selection", "too many", "acknowledgement", "account mode", "organization", "codec"} {
		t.Run(name, func(t *testing.T) {
			s, api, _ := connectionCoreFixture(t)
			req := CreateConnectionRequest{Name: "Candidate", OrganizationID: "org-fixture", AccountMode: "unknown", APIKey: connectionFixtureKey, ProjectIDs: []string{}, AcknowledgeKeyPermissions: true}
			switch name {
			case "missing selection":
				req.ProjectIDs = nil
			case "duplicate selection":
				req.ProjectIDs = []string{"project-one", "project-one"}
			case "too many":
				req.ProjectIDs = []string{"one", "two", "three", "four", "five", "six"}
			case "acknowledgement":
				req.AcknowledgeKeyPermissions = false
			case "account mode":
				req.AccountMode = "inferred-vercel-billing"
			case "organization":
				req.OrganizationID = "../organization"
			case "codec":
				s.Codec.(*connectionCodecFixture).active = false
			}
			api.mu.Lock()
			before := api.projectCalls
			api.mu.Unlock()
			if _, err := s.Create(context.Background(), req); err == nil {
				t.Fatal("invalid onboarding accepted")
			}
			api.mu.Lock()
			after := api.projectCalls
			api.mu.Unlock()
			if after != before {
				t.Fatal("invalid onboarding sent a provider request")
			}
		})
	}
}
