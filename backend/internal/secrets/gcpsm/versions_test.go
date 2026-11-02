package gcpsm

import (
	"context"
	"encoding/json"
	"errors"
	"hash/crc32"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
	"github.com/santapong/cooker/internal/secrets"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const gcpName = "projects/123456789012/secrets/cooker__env__PASSWORD"
const gcpCanary = "SECRET_CANARY_GCP_VALUE"

type fakeVersions struct {
	secret          *secretmanagerpb.Secret
	latest, history *secretmanagerpb.SecretVersion
	value           *secretmanagerpb.AccessSecretVersionResponse
	err             error
	calls           int
	onAccess        func()
	request         string
}

func (f *fakeVersions) GetSecret(context.Context, *secretmanagerpb.GetSecretRequest, ...gax.CallOption) (*secretmanagerpb.Secret, error) {
	f.calls++
	return f.secret, f.err
}
func (f *fakeVersions) GetSecretVersion(_ context.Context, r *secretmanagerpb.GetSecretVersionRequest, _ ...gax.CallOption) (*secretmanagerpb.SecretVersion, error) {
	f.calls++
	if strings.HasSuffix(r.Name, "/latest") {
		return f.latest, f.err
	}
	return f.history, f.err
}
func (f *fakeVersions) AccessSecretVersion(_ context.Context, r *secretmanagerpb.AccessSecretVersionRequest, _ ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	f.calls++
	f.request = r.Name
	if f.onAccess != nil {
		f.onAccess()
	}
	return f.value, f.err
}
func fixture() (*Manager, *fakeVersions) {
	created := timestamppb.New(time.Date(2026, 10, 2, 0, 0, 0, 123, time.UTC))
	v := &secretmanagerpb.SecretVersion{Name: gcpName + "/versions/1", CreateTime: created, State: secretmanagerpb.SecretVersion_ENABLED}
	f := &fakeVersions{secret: &secretmanagerpb.Secret{Name: gcpName, CreateTime: created}, latest: v, history: v, value: &secretmanagerpb.AccessSecretVersionResponse{Name: v.Name, Payload: &secretmanagerpb.SecretPayload{Data: []byte(gcpCanary)}}}
	return &Manager{prefix: "cooker", projectID: "my-project", versions: f}, f
}
func TestGCPExactVersionAcrossRotationAndCanonicalProject(t *testing.T) {
	m, f := fixture()
	r, err := m.Pin(context.Background(), "env", "PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	if r.Resource != gcpName+"/versions/1" || r.Scope.Project != "my-project" {
		t.Fatal("lost canonical/configured identity")
	}
	f.latest = &secretmanagerpb.SecretVersion{Name: gcpName + "/versions/2", CreateTime: timestamppb.Now(), State: secretmanagerpb.SecretVersion_ENABLED}
	b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
	if err != nil || string(b) != gcpCanary {
		t.Fatal("old version unavailable", err)
	}
	if f.request != r.Resource {
		t.Fatal("latest used")
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), gcpCanary) {
		t.Fatal("secret in reference")
	}
}
func TestGCPRejectsScopeBeforeRead(t *testing.T) {
	for _, field := range []string{"authority", "project", "prefix", "env", "key", "provider"} {
		t.Run(field, func(t *testing.T) {
			m, f := fixture()
			r, _ := m.Pin(context.Background(), "env", "PASSWORD")
			f.calls = 0
			switch field {
			case "authority":
				m.authority = "https://other.test"
			case "project":
				m.projectID = "other"
			case "prefix":
				m.prefix = "other"
			case "env":
				r.Scope.EnvironmentID = "other"
			case "key":
				r.Scope.Key = "other"
			case "provider":
				r.Provider = "vault"
			}
			if _, err := m.GetPinned(context.Background(), "env", "PASSWORD", r); !errors.Is(err, secrets.ErrVersionMismatch) {
				t.Fatal(err)
			}
			if f.calls != 0 {
				t.Fatal("provider read before fence")
			}
		})
	}
}
func TestGCPDeletionRecreationIncludingAccessRace(t *testing.T) {
	for _, phase := range []string{"before", "during"} {
		t.Run(phase, func(t *testing.T) {
			m, f := fixture()
			r, _ := m.Pin(context.Background(), "env", "PASSWORD")
			recreate := func() {
				v := proto.Clone(f.history).(*secretmanagerpb.SecretVersion)
				v.CreateTime = timestamppb.New(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
				f.history = v
			}
			if phase == "before" {
				recreate()
			} else {
				f.onAccess = recreate
			}
			b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
			if b != nil || !errors.Is(err, secrets.ErrVersionMismatch) {
				t.Fatal("recreated value escaped", err)
			}
		})
	}
}
func TestGCPMalformedUnavailableAndCanaryErrors(t *testing.T) {
	for _, which := range []string{"error", "disabled", "destroyed", "unknown-state", "nil-version", "nil-value", "nil-payload", "wrong-resource", "bad-crc", "secret-recreated"} {
		t.Run(which, func(t *testing.T) {
			m, f := fixture()
			r, _ := m.Pin(context.Background(), "env", "PASSWORD")
			switch which {
			case "error":
				f.err = errors.New(gcpCanary)
			case "disabled":
				f.history.State = secretmanagerpb.SecretVersion_DISABLED
			case "destroyed":
				f.history.State = secretmanagerpb.SecretVersion_DESTROYED
			case "unknown-state":
				f.history.State = secretmanagerpb.SecretVersion_STATE_UNSPECIFIED
			case "nil-version":
				f.history = nil
			case "nil-value":
				f.value = nil
			case "nil-payload":
				f.value.Payload = nil
			case "wrong-resource":
				f.value.Name = "projects/other/secrets/PASSWORD/versions/1"
			case "bad-crc":
				n := int64(0)
				f.value.Payload.DataCrc32C = &n
			case "secret-recreated":
				f.secret.CreateTime = timestamppb.Now()
			}
			b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
			if err == nil || b != nil || strings.Contains(err.Error(), gcpCanary) {
				t.Fatal("unsafe response")
			}
		})
	}
}
func TestGCPPinRejectsMalformedMetadata(t *testing.T) {
	for _, which := range []string{"nil-secret", "bad-secret", "missing-created", "latest-alias", "zero-version", "invalid-time", "provider-error"} {
		t.Run(which, func(t *testing.T) {
			m, f := fixture()
			switch which {
			case "nil-secret":
				f.secret = nil
			case "bad-secret":
				f.secret.Name = gcpCanary
			case "missing-created":
				f.secret.CreateTime = nil
			case "latest-alias":
				f.latest.Name = gcpName + "/versions/latest"
			case "zero-version":
				f.latest.Name = gcpName + "/versions/0"
			case "invalid-time":
				f.latest.CreateTime = &timestamppb.Timestamp{Seconds: 253402300800}
			case "provider-error":
				f.err = errors.New(gcpCanary)
			}
			r, err := m.Pin(context.Background(), "env", "PASSWORD")
			if err == nil || r.Resource != "" || strings.Contains(err.Error(), gcpCanary) {
				t.Fatal("unsafe pin")
			}
		})
	}
}
func TestGCPChecksumAndEmptyValue(t *testing.T) {
	m, f := fixture()
	r, _ := m.Pin(context.Background(), "env", "PASSWORD")
	f.value.Payload.Data = []byte{}
	n := int64(crc32.Checksum(f.value.Payload.Data, crc32.MakeTable(crc32.Castagnoli)))
	f.value.Payload.DataCrc32C = &n
	b, err := m.GetPinned(context.Background(), "env", "PASSWORD", r)
	if err != nil || b == nil || len(b) != 0 {
		t.Fatal("empty rejected", err)
	}
}

func TestGCPMalformedReferenceRejectedBeforeRead(t *testing.T) {
	for _, which := range []string{"resource", "alias", "incarnation"} {
		t.Run(which, func(t *testing.T) {
			m, f := fixture()
			r, _ := m.Pin(context.Background(), "env", "PASSWORD")
			f.calls = 0
			switch which {
			case "resource":
				r.Resource = "projects/123/secrets/other/versions/1"
			case "alias":
				r.Version = "latest"
			case "incarnation":
				r.Incarnation = gcpCanary
			}
			if _, err := m.GetPinned(context.Background(), "env", "PASSWORD", r); !errors.Is(err, secrets.ErrInvalidVersionReference) {
				t.Fatal(err)
			}
			if f.calls != 0 {
				t.Fatal("malformed reference caused read")
			}
		})
	}
}

func TestGCPEffectiveEndpointBinding(t *testing.T) {
	for _, target := range []string{"secretmanager.googleapis.com:443", "dns:///secretmanager.googleapis.com:443", "passthrough:///secretmanager.googleapis.com:443", "google-c2p:///secretmanager.googleapis.com:443"} {
		if versionAuthority(target) != "https://secretmanager.googleapis.com:443" {
			t.Fatal("target normalization")
		}
	}
	m, f := fixture()
	m.authority = versionAuthority("user:private@example.test")
	if _, err := m.Pin(context.Background(), "env", "PASSWORD"); !errors.Is(err, secrets.ErrInvalidVersionReference) || f.calls != 0 {
		t.Fatal("invalid endpoint read")
	}
}
