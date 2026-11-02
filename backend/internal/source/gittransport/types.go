// Package gittransport implements the shared, restrictive HTTPS source boundary.
// Config must come from an administrator-approved connection, never an event URL.
package gittransport

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type Config struct {
	Origin              string
	APIOrigin           string
	AllowPrivateNetwork bool
	CAPEM               string
}
type Credentials struct {
	Username string
	Token    string
}

func (Credentials) String() string { return "[source credentials]" }

type Repository struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	Origin        string `json:"origin"`
	DefaultBranch string `json:"defaultBranch,omitempty"`
}
type Ref struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}
type Revision struct {
	Ref
	FullRef   string `json:"fullRef"`
	Commit    string `json:"commit"`
	ObjectSHA string `json:"objectSha,omitempty"`
}
type RevisionPage struct {
	Items    []Revision `json:"items"`
	NextPage int        `json:"nextPage,omitempty"`
}

// Adapter does not choose an origin from any repository or webhook response.
// Repository accepts a canonical path or stable numeric provider ID.
type Adapter interface {
	Repository(context.Context, string) (Repository, error)
	ResolveRevision(context.Context, Repository, Ref) (Revision, error)
	VerifyCommit(context.Context, Repository, string) (string, error)
	ListRevisions(context.Context, Repository, string, int) (RevisionPage, error)
	Checkout(context.Context, Repository, string, io.Writer) (string, error)
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
var idPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

func ValidID(id string) bool { return idPattern.MatchString(id) }
func ValidateCommit(sha string) error {
	if !shaPattern.MatchString(sha) || sha == strings.Repeat("0", 40) {
		return fmt.Errorf("source commit must be a full nonzero SHA-1")
	}
	return nil
}
func ValidateRepositoryPath(path string, nested bool) error {
	parts := strings.Split(path, "/")
	if len(path) > 512 || len(parts) < 2 || len(parts) > 20 || (!nested && len(parts) != 2) {
		return fmt.Errorf("invalid repository path")
	}
	for _, p := range parts {
		if !segmentPattern.MatchString(p) || strings.Contains(p, "..") || strings.HasSuffix(p, ".git") || strings.HasSuffix(p, ".") {
			return fmt.Errorf("invalid repository path")
		}
	}
	return nil
}
func (r Ref) FullName() (string, error) {
	prefix := ""
	switch r.Kind {
	case "branch":
		prefix = "refs/heads/"
	case "tag":
		prefix = "refs/tags/"
	default:
		return "", fmt.Errorf("source ref kind must be branch or tag")
	}
	n := r.Name
	if n == "" || len(n) > 1024 || strings.HasPrefix(n, "-") || strings.HasSuffix(n, ".") || strings.Contains(n, "..") || strings.Contains(n, "@{") || strings.ContainsAny(n, " ~^:?*[\\") {
		return "", fmt.Errorf("invalid source ref")
	}
	for _, ch := range n {
		if ch < 0x20 || ch == 0x7f {
			return "", fmt.Errorf("invalid source ref")
		}
	}
	for _, part := range strings.Split(n, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return "", fmt.Errorf("invalid source ref")
		}
	}
	return prefix + n, nil
}
func Resolved(ref Ref, commit string) (Revision, error) {
	full, err := ref.FullName()
	if err != nil {
		return Revision{}, err
	}
	if err = ValidateCommit(commit); err != nil {
		return Revision{}, err
	}
	return Revision{Ref: ref, FullRef: full, Commit: commit}, nil
}
func ValidateCredentials(c Credentials) error {
	if len(c.Token) > 8192 || len(c.Username) > 256 || strings.ContainsAny(c.Username, ":\r\n\x00") || strings.ContainsAny(c.Token, "\r\n\x00") {
		return fmt.Errorf("invalid source credential")
	}
	if c.Token != "" && c.Username == "" {
		return fmt.Errorf("source token requires a Git username")
	}
	if c.Token == "" && c.Username != "" {
		return fmt.Errorf("source username requires a token")
	}
	return nil
}
