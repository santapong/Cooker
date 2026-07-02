package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testFeedbackService(url string) *FeedbackService {
	s := NewFeedbackService("owner/repo", "test-token")
	s.APIBase = url
	return s
}

func TestFeedbackSubmit_HappyPath(t *testing.T) {
	var gotPath string
	var gotHeaders http.Header
	var gotBody struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeaders = r.Header
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"html_url":"https://github.com/owner/repo/issues/42","number":42}`))
	}))
	defer srv.Close()

	// The message carries a triple backtick — the body fence must be
	// longer so the message cannot escape it.
	msg := "the run page broke\n```\n@admin rm -rf\n```"
	issueURL, issueNumber, err := testFeedbackService(srv.URL).Submit(context.Background(), FeedbackInput{
		Category:  "bug",
		Message:   msg,
		PageURL:   "https://cooker.example/pipelines/p1",
		UserAgent: "test-agent/1.0",
		UserID:    "user-123",
		UserEmail: "op@example.com",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if issueURL != "https://github.com/owner/repo/issues/42" || issueNumber != 42 {
		t.Errorf("got %q #%d", issueURL, issueNumber)
	}
	if gotPath != "/repos/owner/repo/issues" {
		t.Errorf("path = %s", gotPath)
	}
	for header, want := range map[string]string{
		"Authorization":        "Bearer test-token",
		"Accept":               "application/vnd.github+json",
		"X-Github-Api-Version": "2022-11-28",
		"Content-Type":         "application/json",
	} {
		if got := gotHeaders.Get(header); got != want {
			t.Errorf("header %s = %q, want %q", header, got, want)
		}
	}
	if len(gotBody.Labels) != 2 || gotBody.Labels[0] != "feedback" || gotBody.Labels[1] != "bug" {
		t.Errorf("labels = %v", gotBody.Labels)
	}
	if gotBody.Title != "[feedback] bug: the run page broke ``` @admin rm -rf ```" {
		t.Errorf("title = %q", gotBody.Title)
	}
	// Metadata lines are present.
	for _, want := range []string{
		"- App: Cooker",
		"- Category: bug",
		"- User: user-123 (op@example.com)",
		"- Page URL: `https://cooker.example/pipelines/p1`",
		"- User-Agent: `test-agent/1.0`",
	} {
		if !strings.Contains(gotBody.Body, want) {
			t.Errorf("body missing %q:\n%s", want, gotBody.Body)
		}
	}
	// The message sits inside a 4-backtick fence (longest run is the
	// triple backtick, +1).
	if !strings.Contains(gotBody.Body, "````\n"+msg+"\n````\n") {
		t.Errorf("message not inside a 4-backtick fence:\n%s", gotBody.Body)
	}
}

// A hostile page URL / User-Agent must not escape onto the bullet line
// as a live @-mention, #-reference or [link](…): both fields are wrapped
// in an inline-code span, and a value carrying its own backticks can't
// close that span early.
func TestFeedbackBody_NeutralizesClientMarkdown(t *testing.T) {
	body := feedbackBody(FeedbackInput{
		Category:  "bug",
		Message:   "hi",
		PageURL:   "@maintainer see [click](https://evil.example) #123",
		UserAgent: "evil`agent`/1.0",
	})
	// The raw @-mention / link / reference must not appear un-wrapped;
	// they only ever appear inside the inline-code span.
	if strings.Contains(body, "- Page URL: @maintainer") {
		t.Errorf("page URL not wrapped in inline code:\n%s", body)
	}
	if !strings.Contains(body, "- Page URL: `@maintainer see [click](https://evil.example) #123`") {
		t.Errorf("page URL span missing/wrong:\n%s", body)
	}
	// The User-Agent carries backticks — the span delimiter must outrun
	// them so the value can't break out.
	if !strings.Contains(body, "- User-Agent: ``evil`agent`/1.0``") {
		t.Errorf("user-agent span did not outrun inner backticks:\n%s", body)
	}
}

func TestFeedbackTitle_Truncation(t *testing.T) {
	tests := []struct {
		name     string
		category string
		message  string
		want     string
	}{
		{"short stays whole", "idea", "dark mode please", "[feedback] idea: dark mode please"},
		{"newlines collapse", "bug", "line one\nline two", "[feedback] bug: line one line two"},
		{
			"long truncates with ellipsis", "other",
			strings.Repeat("a", 61),
			"[feedback] other: " + strings.Repeat("a", 60) + "…",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := feedbackTitle(tt.category, tt.message); got != tt.want {
				t.Errorf("feedbackTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFeedbackFence_OutrunsBacktickRuns(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{"no backticks", "plain text", "```"},
		{"inline code", "use `go vet` here", "```"},
		{"triple backtick", "escape\n```\nattempt", "````"},
		{"five backticks", "`````", "``````"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := feedbackFence(tt.message); got != tt.want {
				t.Errorf("feedbackFence(%q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestFeedbackSubmit_Non201IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer srv.Close()

	_, _, err := testFeedbackService(srv.URL).Submit(context.Background(), FeedbackInput{
		Category: "bug", Message: "it broke",
	})
	if err == nil {
		t.Fatal("want error on 500, got nil")
	}
	if !strings.Contains(err.Error(), "feedback:") {
		t.Errorf("error missing package prefix: %v", err)
	}
}

func TestFeedbackSubmit_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := testFeedbackService(srv.URL).Submit(ctx, FeedbackInput{
		Category: "bug", Message: "it broke",
	})
	if err == nil {
		t.Fatal("want error on context timeout, got nil")
	}
}
