package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// feedbackTimeout bounds one GitHub issue-create call. A hung GitHub
// must fail the request, not pin the handler goroutine.
const feedbackTimeout = 10 * time.Second

// feedbackTitleMax is how many characters of the message make it into
// the issue title before truncation.
const feedbackTitleMax = 60

// FeedbackInput is one feedback submission. UserAgent, UserID and
// UserEmail are derived server-side (request header / auth context),
// never from client JSON.
type FeedbackInput struct {
	Category  string
	Message   string
	PageURL   string
	UserAgent string
	UserID    string
	UserEmail string
}

// FeedbackService relays in-app feedback to GitHub as issues (pure
// relay — nothing is persisted). The token stays server-side; clients
// only ever see the created issue's URL and number.
type FeedbackService struct {
	repo  string
	token string
	// APIBase is the GitHub API root. A struct field (rather than a
	// constant) so tests can point it at an httptest server.
	APIBase string
	client  *http.Client
}

// NewFeedbackService builds the relay for owner/repo with the given
// token (a fine-grained PAT with Issues read/write on that one repo).
func NewFeedbackService(repo, token string) *FeedbackService {
	return &FeedbackService{
		repo:    repo,
		token:   token,
		APIBase: "https://api.github.com",
		client:  &http.Client{Timeout: feedbackTimeout},
	}
}

// Submit files the feedback as a GitHub issue and returns its URL and
// number. Only a 201 from GitHub counts as success; any other status
// is an error whose text may carry GitHub response detail — callers
// must log it, never relay it to clients.
func (s *FeedbackService) Submit(ctx context.Context, in FeedbackInput) (issueURL string, issueNumber int, err error) {
	payload, err := json.Marshal(struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
	}{
		Title:  feedbackTitle(in.Category, in.Message),
		Body:   feedbackBody(in),
		Labels: []string{"feedback", in.Category},
	})
	if err != nil {
		return "", 0, fmt.Errorf("feedback: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.APIBase+"/repos/"+s.repo+"/issues", bytes.NewReader(payload))
	if err != nil {
		return "", 0, fmt.Errorf("feedback: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("feedback: request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, fmt.Errorf("feedback: read response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		detail := string(raw)
		if len(detail) > 512 {
			detail = detail[:512]
		}
		return "", 0, fmt.Errorf("feedback: github returned %d: %s", resp.StatusCode, detail)
	}
	var out struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", 0, fmt.Errorf("feedback: decode response: %w", err)
	}
	return out.HTMLURL, out.Number, nil
}

// feedbackTitle is "[feedback] <category>: <first 60 chars of the
// message>", whitespace runs (newlines included) collapsed to single
// spaces, "…" appended when truncated.
func feedbackTitle(category, message string) string {
	line := collapseSpace(message)
	if r := []rune(line); len(r) > feedbackTitleMax {
		line = string(r[:feedbackTitleMax]) + "…"
	}
	return fmt.Sprintf("[feedback] %s: %s", category, line)
}

// feedbackBody renders the metadata lines then the message inside a
// dynamic code fence (see feedbackFence) so a hostile message cannot
// escape into markdown, @-mentions or a fence of its own. Client-
// controlled metadata (the page URL and the request User-Agent) is
// wrapped in an inline-code span (see inlineCode) for the same reason —
// collapseSpace alone would leave @-mentions, #-references and
// [links](…) live on the bullet line.
func feedbackBody(in FeedbackInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- App: Cooker\n")
	fmt.Fprintf(&b, "- Category: %s\n", in.Category)
	fmt.Fprintf(&b, "- User: %s (%s)\n", collapseSpace(in.UserID), collapseSpace(in.UserEmail))
	fmt.Fprintf(&b, "- Page URL: %s\n", inlineCode(collapseSpace(in.PageURL)))
	fmt.Fprintf(&b, "- User-Agent: %s\n", inlineCode(collapseSpace(in.UserAgent)))
	fmt.Fprintf(&b, "- Submitted: %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fence := feedbackFence(in.Message)
	b.WriteString(fence + "\n" + in.Message + "\n" + fence + "\n")
	return b.String()
}

// feedbackFence returns a backtick fence longer than any backtick run
// inside s — max(3, longest run + 1) — so the message can never close
// the fence early.
func feedbackFence(s string) string {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := longest + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

// collapseSpace flattens whitespace runs (newlines included) to single
// spaces so client-influenced metadata stays on its own bullet line.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// inlineCode wraps client-controlled metadata in a GitHub inline-code
// span so it renders as literal text — no markdown, @-mentions,
// #-references or [links](…) can escape onto the bullet line. The
// backtick delimiter is one longer than the longest backtick run inside
// s (mirroring feedbackFence) so the value can't close the span early;
// a value that begins or ends with a backtick is padded with a space,
// which CommonMark strips from a code span whose content isn't all
// spaces. Empty input stays empty.
func inlineCode(s string) string {
	if s == "" {
		return ""
	}
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	ticks := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return ticks + " " + s + " " + ticks
	}
	return ticks + s + ticks
}
