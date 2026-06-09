package service

import (
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func TestPipelineRunDeadline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty → no override", "", 0},
		{"garbage → no override", "tomorrow", 0},
		{"negative → no override", "-5m", 0},
		{"valid passes through", "45m", 45 * time.Minute},
		{"below floor clamps", "2s", MinRunDeadline},
		{"above ceiling clamps", "48h", MaxRunDeadline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PipelineRunDeadline(&model.Pipeline{RunDeadline: tc.in})
			if got != tc.want {
				t.Errorf("PipelineRunDeadline(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
	if PipelineRunDeadline(nil) != 0 {
		t.Error("nil pipeline must yield 0")
	}
}
