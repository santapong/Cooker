package cloudrun

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestIsNotFound pins the branch-selection classifier the Deploy path
// relies on: only a genuine gRPC NotFound status should route to
// CreateService; every other error (or a nil error) must not be treated
// as "service absent" (NEW-4 — mirror the ECS adapter's errors.As gate).
func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"plain not-found status", status.Error(codes.NotFound, "service missing"), true},
		{"wrapped not-found status", fmt.Errorf("get: %w", status.Error(codes.NotFound, "service missing")), true},
		{"permission denied", status.Error(codes.PermissionDenied, "IAM denied"), false},
		{"unavailable / throttled", status.Error(codes.Unavailable, "throttled"), false},
		{"invalid argument (wrong region)", status.Error(codes.InvalidArgument, "bad parent"), false},
		{"non-grpc error", errors.New("dial tcp: connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNotFound(tt.err); got != tt.want {
				t.Fatalf("isNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
