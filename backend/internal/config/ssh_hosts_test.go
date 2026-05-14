package config

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubLister is a test-side SSHHostLister.
type stubLister struct {
	out []SSHHostSummary
	err error
}

func (s stubLister) ListSSHHostsLaxStrictHostKey(_ context.Context) ([]SSHHostSummary, error) {
	return s.out, s.err
}

func TestValidateSSHHosts_NonProductionIsNoop(t *testing.T) {
	c := &Config{Env: EnvDev}
	if err := c.ValidateSSHHosts(context.Background(), stubLister{
		out: []SSHHostSummary{{ID: "h-1", Name: "lax"}},
	}); err != nil {
		t.Fatalf("dev should be a no-op, got %v", err)
	}
}

func TestValidateSSHHosts_ProductionWithNoLaxHostsOK(t *testing.T) {
	c := &Config{Env: EnvProduction}
	if err := c.ValidateSSHHosts(context.Background(), stubLister{out: nil}); err != nil {
		t.Fatalf("empty-list production should pass, got %v", err)
	}
}

func TestValidateSSHHosts_ProductionWithLaxHostsRefused(t *testing.T) {
	c := &Config{Env: EnvProduction}
	err := c.ValidateSSHHosts(context.Background(), stubLister{
		out: []SSHHostSummary{
			{ID: "h-1", Name: "vm-1"},
			{ID: "h-2", Name: "vm-2"},
		},
	})
	if err == nil {
		t.Fatal("expected refusal in production with lax hosts")
	}
	if !strings.Contains(err.Error(), "vm-1") || !strings.Contains(err.Error(), "vm-2") {
		t.Errorf("error should name offending hosts, got %v", err)
	}
}

func TestValidateSSHHosts_ListerErrorPropagates(t *testing.T) {
	c := &Config{Env: EnvProduction}
	wantErr := errors.New("db down")
	err := c.ValidateSSHHosts(context.Background(), stubLister{err: wantErr})
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped lister error, got %v", err)
	}
}

func TestValidateSSHHosts_NilListerNoop(t *testing.T) {
	c := &Config{Env: EnvProduction}
	if err := c.ValidateSSHHosts(context.Background(), nil); err != nil {
		t.Fatalf("nil lister should be a no-op, got %v", err)
	}
}
