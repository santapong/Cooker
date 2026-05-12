// Package validate centralises the per-field input validation that
// handlers used to skip. Every handler that accepts user input
// should call into here post-bind and return 400 on the first
// error.
//
// Each function returns a non-nil error on rejection; the error
// string is shaped for direct use as the HTTP body's "error" field.
package validate

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/santapong/cooker/internal/model"
)

// Length caps. Aligned with reasonable PG TEXT usage and avoid
// JSONB bloat for free-text fields that accidentally accept paste-
// wars from a confused UI.
const (
	MaxNameLen        = 255
	MaxDescriptionLen = 2000
)

var (
	// owner/repo on GitHub. Owners are alphanum/underscore/hyphen,
	// 1-39 chars; repo names allow letters, numbers, dot, underscore,
	// hyphen.
	githubRepoRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)
	// Docker tag character class per OCI distribution spec
	// (component allowed chars; we don't validate the registry
	// host portion which is optional).
	dockerTagRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	// Git ref name — git's documented rules condensed: no spaces,
	// no `..`, no leading slash, no leading dot. Liberal but blocks
	// the obvious paste mistakes.
	gitRefRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// Name caps the length of a free-text name and rejects empty.
func Name(field, s string) error {
	if s == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(s) > MaxNameLen {
		return fmt.Errorf("%s exceeds %d characters", field, MaxNameLen)
	}
	return nil
}

// Description caps the length of a description (empty allowed).
func Description(field, s string) error {
	if len(s) > MaxDescriptionLen {
		return fmt.Errorf("%s exceeds %d characters", field, MaxDescriptionLen)
	}
	return nil
}

// StageType rejects any value that isn't a known model.StageType.
func StageType(t model.StageType) error {
	switch t {
	case model.StageTypeBuild, model.StageTypeTest, model.StageTypeDeploy,
		model.StageTypePush, model.StageTypeApproval, model.StageTypeCustom,
		model.StageTypeGitOpsCommit:
		return nil
	}
	return fmt.Errorf("unknown stage type %q", t)
}

// HostKind rejects any value that isn't a known model.HostKind.
func HostKind(k model.HostKind) error {
	switch k {
	case model.HostKindDocker, model.HostKindKubernetes:
		return nil
	}
	return fmt.Errorf("unknown host kind %q", k)
}

// HostReachability rejects any value that isn't direct/tailnet.
func HostReachability(r model.HostReachability) error {
	switch r {
	case model.HostDirect, model.HostTailnet:
		return nil
	}
	return fmt.Errorf("unknown host reachability %q", r)
}

// EnvironmentTargetType rejects values outside cluster/namespace.
func EnvironmentTargetType(t string) error {
	switch t {
	case "", "cluster", "namespace":
		return nil
	}
	return fmt.Errorf("environment target type must be cluster or namespace, got %q", t)
}

// GitHubRepo enforces "owner/name" syntax.
func GitHubRepo(s string) error {
	if !githubRepoRe.MatchString(s) {
		return fmt.Errorf("githubRepo %q is not in owner/name form", s)
	}
	return nil
}

// GitRefName accepts ordinary branch / tag names.
func GitRefName(field, s string) error {
	if s == "" {
		return nil
	}
	if strings.Contains(s, "..") || strings.HasSuffix(s, "/") {
		return fmt.Errorf("%s contains an invalid sequence", field)
	}
	if !gitRefRe.MatchString(s) {
		return fmt.Errorf("%s is not a valid git ref", field)
	}
	return nil
}

// DockerTag validates a single image tag (the part after the colon).
func DockerTag(s string) error {
	if !dockerTagRe.MatchString(s) {
		return fmt.Errorf("docker tag %q has invalid characters", s)
	}
	return nil
}

// DockerTags validates each entry of a tag list.
func DockerTags(tags []string) error {
	for i, t := range tags {
		// Strip an optional <registry>/<repo>: prefix; the part after
		// the last colon is the actual tag.
		tag := t
		if i := strings.LastIndex(t, ":"); i >= 0 {
			tag = t[i+1:]
		}
		if err := DockerTag(tag); err != nil {
			return fmt.Errorf("tags[%d]: %w", i, err)
		}
	}
	return nil
}

// HTTPSURL accepts http://localhost… in dev but only https:// when
// requireHTTPS is true (production policy gate at the call site).
func HTTPSURL(field, s string, requireHTTPS bool) error {
	if s == "" {
		return fmt.Errorf("%s is required", field)
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", field, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must include scheme and host", field)
	}
	if requireHTTPS && u.Scheme != "https" {
		return fmt.Errorf("%s must use https", field)
	}
	return nil
}
