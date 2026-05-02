package deploytarget_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/cooker-ci/cooker/internal/deploytarget"
	"github.com/cooker-ci/cooker/internal/deploytarget/cloudrun"
	"github.com/cooker-ci/cooker/internal/model"
)

type fakeTarget struct{ kind model.DeployTargetKind }

func (f *fakeTarget) Kind() model.DeployTargetKind                  { return f.kind }
func (*fakeTarget) Deploy(context.Context, deploytarget.Spec) error { return nil }
func (*fakeTarget) Status(context.Context, string) (deploytarget.Status, error) {
	return deploytarget.Status{Healthy: true}, nil
}
func (*fakeTarget) Logs(context.Context, string, io.Writer) error { return nil }
func (*fakeTarget) Rollback(context.Context, string) error        { return nil }

func TestRegisterAndLookup(t *testing.T) {
	deploytarget.ResetForTest()
	f := &fakeTarget{kind: model.DeployTargetKubernetes}
	if err := deploytarget.Register(f); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := deploytarget.Lookup(model.DeployTargetKubernetes)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != model.DeployTargetKubernetes {
		t.Errorf("wrong kind: %s", got.Kind())
	}
}

func TestLookup_UnknownReturnsErrUnavailable(t *testing.T) {
	deploytarget.ResetForTest()
	_, err := deploytarget.Lookup(model.DeployTargetCloudRun)
	if !errors.Is(err, deploytarget.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestRegister_DuplicateKindReturnsError(t *testing.T) {
	deploytarget.ResetForTest()
	if err := deploytarget.Register(&fakeTarget{kind: model.DeployTargetDockerHost}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := deploytarget.Register(&fakeTarget{kind: model.DeployTargetDockerHost})
	if !errors.Is(err, deploytarget.ErrDuplicateKind) {
		t.Errorf("expected ErrDuplicateKind, got %v", err)
	}
}

func TestMustRegister_DuplicateKindPanics(t *testing.T) {
	deploytarget.ResetForTest()
	deploytarget.MustRegister(&fakeTarget{kind: model.DeployTargetDockerHost})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate MustRegister")
		}
	}()
	deploytarget.MustRegister(&fakeTarget{kind: model.DeployTargetDockerHost})
}

func TestCloudRun_KindAndConstructor(t *testing.T) {
	// Smoke test: constructor returns a Target whose Kind is
	// cloud-run. Real Deploy/Status calls hit Google's gRPC API and
	// ADC discovery, which is not safe to exercise in unit tests on
	// hosts without metadata servers — covered by integration tests
	// against a real GCP project (follow-up).
	deploytarget.ResetForTest()
	tt := cloudrun.New("proj", "us-central1")
	if tt.Kind() != model.DeployTargetCloudRun {
		t.Errorf("Kind() = %q, want cloud-run", tt.Kind())
	}
}

func TestCloudRunStub_MissingProjectReportsErrUnavailable(t *testing.T) {
	tt := cloudrun.New("", "")
	_, err := tt.Status(context.Background(), "x")
	if !errors.Is(err, deploytarget.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}
