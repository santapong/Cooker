package validate

import (
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestRunDeadline(t *testing.T) {
	for _, ok := range []string{"", "10s", "45m", "2h", "24h"} {
		if err := RunDeadline(ok); err != nil {
			t.Errorf("RunDeadline(%q): unexpected error %v", ok, err)
		}
	}
	for _, bad := range []string{"soon", "5s", "25h", "-1h", "45"} {
		if err := RunDeadline(bad); err == nil {
			t.Errorf("RunDeadline(%q): expected rejection", bad)
		}
	}
}

func TestRetryPolicyValidation(t *testing.T) {
	ok := []*model.RetryPolicy{
		nil,
		{},
		{MaxAttempts: 10},
		{MaxAttempts: 3, InitialMS: 500, MaxMS: 5000},
		{InitialMS: 60_000, MaxMS: 300_000},
	}
	for i, r := range ok {
		if err := RetryPolicy(r); err != nil {
			t.Errorf("ok[%d]: unexpected error %v", i, err)
		}
	}
	bad := []*model.RetryPolicy{
		{MaxAttempts: 11},
		{MaxAttempts: -1},
		{InitialMS: 60_001},
		{MaxMS: 300_001},
		{InitialMS: 5000, MaxMS: 1000}, // max below initial
	}
	for i, r := range bad {
		if err := RetryPolicy(r); err == nil {
			t.Errorf("bad[%d]: expected rejection of %+v", i, r)
		}
	}
}
