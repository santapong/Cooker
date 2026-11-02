package connection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/crypto"
)

func replacementFixture(t *testing.T) (*Service, *Connection) {
	t.Helper()
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{Store: NewMemory(), Codec: codec}
	c := &Connection{Name: "Owned token source", Provider: "gitea", Origin: "https://git.example.test", AuthMode: "token", Username: "old-user", Repositories: []string{"team/one", "team/two"}, Enabled: false}
	if err := s.CreateWithCredentials(context.Background(), c, Credentials{Token: "synthetic-old-token", WebhookSecret: strings.Repeat("h", 32)}); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func TestReplaceTokenChecksCandidateBeforeSavingAndPreservesConnection(t *testing.T) {
	s, before := replacementFixture(t)
	calls := 0
	s.TokenProbe = func(ctx context.Context, candidate *Connection) error {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 20*time.Second {
			t.Fatal("unbounded token check")
		}
		saved, err := s.Store.Get(ctx, before.ID)
		if err != nil || !reflect.DeepEqual(saved, before) {
			t.Fatal("candidate saved before its access check")
		}
		value, err := s.OpenCredentials(candidate)
		if err != nil || value.Token != "synthetic-new-token" || value.WebhookSecret != strings.Repeat("h", 32) || candidate.Username != "new-user" {
			t.Fatal("wrong replacement credentials")
		}
		candidate.Name = "probe cannot edit the saved candidate"
		candidate.Repositories[0] = "other/repo"
		clear(candidate.Credential)
		return nil
	}
	username := "new-user"
	after, err := s.ReplaceToken(context.Background(), before.ID, before.Version, "synthetic-new-token", &username)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || after.Version != before.Version+1 || after.Enabled || after.Name != before.Name || after.Origin != before.Origin || !reflect.DeepEqual(after.Repositories, before.Repositories) || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatal("replacement changed unrelated configuration or omitted version fencing")
	}
	value, err := s.OpenCredentials(after)
	if err != nil || value.Token != "synthetic-new-token" || value.WebhookSecret != strings.Repeat("h", 32) {
		t.Fatal("replacement lost credentials")
	}
	wire, _ := json.Marshal(after)
	for _, private := range []string{"synthetic-new-token", "synthetic-old-token", strings.Repeat("h", 32)} {
		if strings.Contains(string(wire), private) {
			t.Fatal("private value in response")
		}
	}
	stored, _ := s.Store.Get(context.Background(), before.ID)
	if !reflect.DeepEqual(after, stored) {
		t.Fatal("returned connection differs from saved connection")
	}
}

func TestReplaceTokenFailureAndCancellationDoNotChangeSavedState(t *testing.T) {
	for _, mode := range []string{"unavailable", "provider-error", "cancel-during-probe", "cancel-before", "stale", "empty", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			s, before := replacementFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s.TokenProbe = func(context.Context, *Connection) error {
				calls++
				if mode == "cancel-during-probe" {
					cancel()
					return nil
				}
				return errors.New("synthetic-private-provider-body")
			}
			version, token, wantCalls := before.Version, "synthetic-new-token", 0
			switch mode {
			case "unavailable":
				s.TokenProbe = nil
			case "cancel-before":
				cancel()
			case "stale":
				version++
			case "empty":
				token = ""
			case "malformed":
				token = "invalid\ntoken"
			default:
				wantCalls = 1
			}
			_, err := s.ReplaceToken(ctx, before.ID, version, token, nil)
			if err == nil || strings.Contains(err.Error(), "synthetic-private-provider-body") || calls != wantCalls {
				t.Fatal("replacement did not safely refuse its boundary")
			}
			saved, _ := s.Store.Get(context.Background(), before.ID)
			if !reflect.DeepEqual(saved, before) {
				t.Fatal("failed replacement changed saved connection")
			}
		})
	}
}

func TestReplaceTokenConcurrentEditCannotBeOverwritten(t *testing.T) {
	s, before := replacementFixture(t)
	var edited *Connection
	s.TokenProbe = func(ctx context.Context, _ *Connection) error {
		var err error
		edited, err = s.Store.Get(ctx, before.ID)
		if err != nil {
			return err
		}
		edited.Name = "Concurrent edit"
		return s.Store.Update(ctx, edited)
	}
	_, err := s.ReplaceToken(context.Background(), before.ID, before.Version, "synthetic-new-token", nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatal("concurrent source edit was overwritten")
	}
	saved, _ := s.Store.Get(context.Background(), before.ID)
	if !reflect.DeepEqual(saved, edited) {
		t.Fatal("replacement changed concurrent edit")
	}
	value, _ := s.OpenCredentials(saved)
	if value.Token != "synthetic-old-token" {
		t.Fatal("conflicting replacement persisted")
	}
}

func TestGenericTokenUpdateCannotBypassCheck(t *testing.T) {
	s, before := replacementFixture(t)
	candidate, _ := s.Store.Get(context.Background(), before.ID)
	token := "synthetic-new-token"
	if !errors.Is(s.UpdateWithCredentials(context.Background(), candidate, CredentialsPatch{Token: &token}), ErrCredentialCheckUnavailable) {
		t.Fatal("generic update bypassed token preflight")
	}
	saved, _ := s.Store.Get(context.Background(), before.ID)
	if !reflect.DeepEqual(saved, before) {
		t.Fatal("generic unverified update changed state")
	}
}

func TestReplaceTokenRefusesOtherAuthModes(t *testing.T) {
	for _, mode := range []string{"anonymous", "github_app"} {
		t.Run(mode, func(t *testing.T) {
			s, c := replacementFixture(t)
			c.AuthMode = mode
			c.Username = ""
			c.HasToken = false
			if err := s.Store.Update(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			calls := 0
			s.TokenProbe = func(context.Context, *Connection) error { calls++; return nil }
			if _, err := s.ReplaceToken(context.Background(), c.ID, c.Version, "synthetic-new-token", nil); !errors.Is(err, ErrTokenReplacementInput) || calls != 0 {
				t.Fatal("token replacement changed another auth mode")
			}
		})
	}
}
