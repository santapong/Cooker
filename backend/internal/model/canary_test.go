package model

import (
	"errors"
	"testing"
)

func TestCanaryConfig_Normalize(t *testing.T) {
	tests := []struct {
		name string
		in   CanaryConfig
		want CanaryConfig
	}{
		{
			name: "empty becomes rolling",
			in:   CanaryConfig{},
			want: CanaryConfig{Strategy: DeployStrategyRolling},
		},
		{
			name: "rolling drops stale canary knobs",
			in:   CanaryConfig{Strategy: DeployStrategyRolling, Weight: 40, AutoPromote: true, HealthWindowSeconds: 99},
			want: CanaryConfig{Strategy: DeployStrategyRolling},
		},
		{
			name: "canary without weight gets default",
			in:   CanaryConfig{Strategy: DeployStrategyCanary},
			want: CanaryConfig{Strategy: DeployStrategyCanary, Weight: DefaultCanaryWeight},
		},
		{
			name: "canary auto-promote without window gets default",
			in:   CanaryConfig{Strategy: DeployStrategyCanary, Weight: 25, AutoPromote: true},
			want: CanaryConfig{Strategy: DeployStrategyCanary, Weight: 25, AutoPromote: true, HealthWindowSeconds: DefaultCanaryHealthWindowSeconds},
		},
		{
			name: "manual canary keeps zero window",
			in:   CanaryConfig{Strategy: DeployStrategyCanary, Weight: 25, AutoPromote: false},
			want: CanaryConfig{Strategy: DeployStrategyCanary, Weight: 25},
		},
		{
			name: "explicit values preserved",
			in:   CanaryConfig{Strategy: DeployStrategyCanary, Weight: 50, AutoPromote: true, HealthWindowSeconds: 120},
			want: CanaryConfig{Strategy: DeployStrategyCanary, Weight: 50, AutoPromote: true, HealthWindowSeconds: 120},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Normalize(); got != tc.want {
				t.Errorf("Normalize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCanaryConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		in        CanaryConfig
		wantErr   bool
		wantField string
	}{
		{name: "empty ok", in: CanaryConfig{}},
		{name: "rolling ok", in: CanaryConfig{Strategy: DeployStrategyRolling}},
		{name: "canary in range ok", in: CanaryConfig{Strategy: DeployStrategyCanary, Weight: 10}},
		{name: "canary at lower bound ok", in: CanaryConfig{Strategy: DeployStrategyCanary, Weight: 1}},
		{name: "canary at upper bound ok", in: CanaryConfig{Strategy: DeployStrategyCanary, Weight: 99}},
		{name: "canary weight zero rejected", in: CanaryConfig{Strategy: DeployStrategyCanary, Weight: 0}, wantErr: true, wantField: "canaryWeight"},
		{name: "canary weight 100 rejected", in: CanaryConfig{Strategy: DeployStrategyCanary, Weight: 100}, wantErr: true, wantField: "canaryWeight"},
		{name: "unknown strategy rejected", in: CanaryConfig{Strategy: "blue-green"}, wantErr: true, wantField: "deployStrategy"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.wantErr != (err != nil) {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				var cerr *CanaryConfigError
				if !errors.As(err, &cerr) {
					t.Fatalf("expected *CanaryConfigError, got %T", err)
				}
				if cerr.Field != tc.wantField {
					t.Errorf("err field = %q, want %q", cerr.Field, tc.wantField)
				}
			}
		})
	}
}

func TestCanaryStatus_IsTerminal(t *testing.T) {
	terminal := []CanaryStatus{CanaryPromoted, CanaryAborted, CanaryFailed}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	if CanaryProgressing.IsTerminal() {
		t.Error("progressing must not be terminal")
	}
}

func TestCanaryConfig_IsCanary(t *testing.T) {
	if !(CanaryConfig{Strategy: DeployStrategyCanary}).IsCanary() {
		t.Error("canary strategy should report IsCanary")
	}
	if (CanaryConfig{}).IsCanary() {
		t.Error("empty strategy should not report IsCanary")
	}
	if (CanaryConfig{Strategy: DeployStrategyRolling}).IsCanary() {
		t.Error("rolling should not report IsCanary")
	}
}
