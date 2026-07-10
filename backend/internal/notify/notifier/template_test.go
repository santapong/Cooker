package notifier

import (
	"strings"
	"testing"
)

func TestRenderTitlePerEventType(t *testing.T) {
	cases := map[EventType]string{
		EventRunSucceeded:    "Run succeeded",
		EventRunFailed:       "Run failed",
		EventRunCancelled:    "Run cancelled",
		EventDeploySucceeded: "Deploy succeeded",
		EventDeployFailed:    "Deploy failed",
		EventBuildFailed:     "Build failed",
	}
	for et, want := range cases {
		title := renderTitle(Event{Type: et, PipelineName: "foo", AppName: "bar", Environment: "prod"})
		if !strings.Contains(title, want) {
			t.Errorf("%s: title=%q does not contain %q", et, title, want)
		}
	}
}

func TestRenderBodyIncludesError(t *testing.T) {
	body := renderBody(Event{Type: EventRunFailed, PipelineName: "x", RunID: "r1", Error: "connection refused"})
	if !strings.Contains(body, "connection refused") {
		t.Fatalf("body=%q missing error", body)
	}
}

func TestRenderBodyIncludesURLWhenSet(t *testing.T) {
	body := renderBody(Event{Type: EventRunFailed, PipelineName: "x", RunID: "r1", URL: "https://cooker/r/1"})
	if !strings.Contains(body, "https://cooker/r/1") {
		t.Fatalf("body=%q missing URL", body)
	}
}

func TestRenderTitleUnknownEventTypeFallback(t *testing.T) {
	title := renderTitle(Event{Type: EventType("made.up")})
	if !strings.Contains(title, "made.up") {
		t.Fatalf("unknown event fell back without preserving type: %q", title)
	}
}
