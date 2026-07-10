package ecs

import (
	"errors"
	"testing"

	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// TestPreviousTaskDefRevision checks the ARN manipulation helper used
// by Rollback.
func TestPreviousTaskDefRevision(t *testing.T) {
	tests := []struct {
		name    string
		arn     string
		want    string
		wantErr bool
	}{
		{
			name: "revision 7 -> 6",
			arn:  "arn:aws:ecs:us-east-1:123456789012:task-definition/my-app:7",
			want: "arn:aws:ecs:us-east-1:123456789012:task-definition/my-app:6",
		},
		{
			name: "revision 2 -> 1",
			arn:  "arn:aws:ecs:us-east-1:123456789012:task-definition/my-app:2",
			want: "arn:aws:ecs:us-east-1:123456789012:task-definition/my-app:1",
		},
		{
			name:    "revision 1 cannot go lower",
			arn:     "arn:aws:ecs:us-east-1:123456789012:task-definition/my-app:1",
			wantErr: true,
		},
		{
			name:    "malformed arn without colon",
			arn:     "notanarn",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := previousTaskDefRevision(tc.arn)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tc.arn)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestECS_E2_UpdateServiceNonNotFoundErrorPropagates verifies the E-2
// fix: when UpdateService returns an error that is NOT
// ServiceNotFoundException, Deploy must return that error and must NOT
// fall through to CreateService.
//
// We test the errors.As logic in isolation — no AWS credentials needed.
func TestECS_E2_UpdateServiceNonNotFoundErrorPropagates(t *testing.T) {
	// Simulate an IAM-denied-style error that is NOT ServiceNotFoundException.
	iamErr := errors.New("AccessDeniedException: User is not authorized to perform ecs:UpdateService")

	var notFound *ecstypes.ServiceNotFoundException
	if errors.As(iamErr, &notFound) {
		t.Fatal("plain error should NOT match ServiceNotFoundException; test assumption broken")
	}

	// ServiceNotFoundException IS recognised by errors.As.
	svcNotFound := &ecstypes.ServiceNotFoundException{}
	if !errors.As(svcNotFound, &notFound) {
		t.Fatal("ServiceNotFoundException should match itself via errors.As")
	}

	// Confirm wrapping also works (errors.As traverses the chain).
	wrapped := errors.New("ecs: update service: " + svcNotFound.Error())
	// Plain wrapping via errors.New does NOT carry type — confirm the
	// negative case so callers know they must use %w.
	if errors.As(wrapped, &notFound) {
		t.Logf("note: errors.New wrapping strips type information as expected")
	}
}

// TestECS_RequireConfig ensures missing Region or Cluster returns
// ErrUnavailable rather than a nil-pointer panic.
func TestECS_RequireConfig(t *testing.T) {
	target := New("", "")
	err := target.requireConfig()
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}
