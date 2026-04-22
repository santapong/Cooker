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

func (f *fakeTarget) Kind() model.DeployTargetKind                         { return f.kind }
func (*fakeTarget) Deploy(context.Context, deploytarget.Spec) error        { return nil }
func (*fakeTarget) Status(context.Context, string) (deploytarget.Status, error) { return deploytarget.Status{Healthy: true}, nil }
func (*fakeTarget) Logs(context.Context, string, io.Writer) error          { return nil }
func (*fakeTarget) Rollback(context.Context, string) error                 { return nil }

func TestRegisterAndLookup(t *testing.T) {
	deploytarget.ResetForTest()
	f := &fakeTarget{kind: model.DeployTargetKubernetes}
	deploytarget.Register(f)

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

func TestRegister_DuplicateKindPanics(t *testing.T) {
	deploytarget.ResetForTest()
	deploytarget.Register(&fakeTarget{kind: model.DeployTargetDockerHost})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	deploytarget.Register(&fakeTarget{kind: model.DeployTargetDockerHost})
}

func TestCloudRunStub_ReportsErrUnavailable(t *testing.T) {
	deploytarget.ResetForTest()
	tt := cloudrun.New("proj", "us-central1")
	err := tt.Deploy(context.Background(), deploytarget.Spec{AppID: "x"})
	if !errors.Is(err, deploytarget.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestCloudRunStub_MissingProjectReportsErrUnavailable(t *testing.T) {
	tt := cloudrun.New("", "")
	_, err := tt.Status(context.Background(), "x")
	if !errors.Is(err, deploytarget.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}
