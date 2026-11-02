package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReferenceValidationAndScope(t *testing.T) {
	r := VersionReference{SchemaVersion: 1, Provider: "awsm", Resource: "arn:example", Version: "version", Scope: VersionScope{Authority: "aws-secrets-manager", EnvironmentID: "env", Key: "PASSWORD"}}
	if err := r.ValidateScope("awsm", r.Scope); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*VersionReference){
		func(r *VersionReference) { r.SchemaVersion = 2 },
		func(r *VersionReference) { r.Provider = "database" },
		func(r *VersionReference) { r.Resource = strings.Repeat("a", 1025) },
		func(r *VersionReference) { r.Version = "bad\nversion" },
		func(r *VersionReference) { r.Incarnation = strings.Repeat("a", 257) },
		func(r *VersionReference) { r.Scope.Authority = "https://user:password@example.test" },
		func(r *VersionReference) { r.Scope.Authority = "https://example.test?token=private" },
		func(r *VersionReference) { r.Scope.Key = "" },
	} {
		bad := r
		change(&bad)
		if !errors.Is(bad.Validate(), ErrInvalidVersionReference) {
			t.Fatal("invalid reference accepted")
		}
	}
	for _, field := range []string{"authority", "region", "project", "mount", "prefix", "env", "key", "provider"} {
		t.Run(field, func(t *testing.T) {
			bad := r
			switch field {
			case "authority":
				bad.Scope.Authority = "other"
			case "region":
				bad.Scope.Region = "other"
			case "project":
				bad.Scope.Project = "other"
			case "mount":
				bad.Scope.Mount = "other"
			case "prefix":
				bad.Scope.Prefix = "other"
			case "env":
				bad.Scope.EnvironmentID = "other"
			case "key":
				bad.Scope.Key = "other"
			case "provider":
				bad.Provider = "gcpsm"
			}
			if !errors.Is(bad.ValidateScope("awsm", r.Scope), ErrVersionMismatch) {
				t.Fatal("scope accepted")
			}
		})
	}
}

type unversioned struct{ gets int }

func (m *unversioned) Get(context.Context, string, string) ([]byte, error) {
	m.gets++
	return []byte("latest"), nil
}
func (*unversioned) Put(context.Context, string, string, []byte) error { return nil }
func (*unversioned) Delete(context.Context, string, string) error      { return nil }
func (*unversioned) List(context.Context, string) ([]string, error)    { return nil, nil }
func TestUnsupportedNeverFallsBack(t *testing.T) {
	m := &unversioned{}
	if _, err := PinVersion(context.Background(), m, "e", "k"); !errors.Is(err, ErrVersionUnsupported) {
		t.Fatal(err)
	}
	if _, err := ReadPinned(context.Background(), m, "e", "k", VersionReference{}); !errors.Is(err, ErrVersionUnsupported) {
		t.Fatal(err)
	}
	if m.gets != 0 {
		t.Fatal("latest fallback")
	}
}
func TestAuthorityAndLogicalPaths(t *testing.T) {
	got, err := CanonicalAuthority("HTTPS://EXAMPLE.test/base/")
	if err != nil || got != "https://example.test/base" {
		t.Fatal(got, err)
	}
	for _, p := range []string{"../x", "a//b", "a/../b", "a?token=x"} {
		if ValidPath(p, false) {
			t.Fatal("bad path accepted")
		}
	}
	if !ValidPath("prefix/env/key", false) || !ValidPath("", true) {
		t.Fatal("good paths rejected")
	}
}
