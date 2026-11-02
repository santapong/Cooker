package model

import (
	"path"
	"strings"
)

// Matches compares authenticated provider/repository identity and typed refs.
// Shared by inbox projection and atomic admission so policy cannot drift.
func (r TriggerRule) Matches(event VerifiedGitEvent) (bool, string) {
	if !r.Source.SameRepository(event.Source) {
		return false, "repository does not match"
	}
	if !r.Enabled {
		return false, "rule is disabled"
	}
	if r.RefKind != "branch" && r.RefKind != "tag" || r.Match != "exact" && r.Match != "glob" {
		return false, "rule ref policy is invalid"
	}
	prefix := "refs/heads/"
	if r.RefKind == "tag" {
		prefix = "refs/tags/"
	}
	if !strings.HasPrefix(event.FullRef, prefix) {
		return false, "ref kind does not match"
	}
	name := strings.TrimPrefix(event.FullRef, prefix)
	if name == "" {
		return false, "ref name is unavailable"
	}
	match := func(pattern string) bool {
		if r.Match == "exact" {
			return pattern == name
		}
		ok, _ := path.Match(pattern, name)
		return ok
	}
	for _, pattern := range r.Exclude {
		if match(pattern) {
			return false, "ref matches an exclusion"
		}
	}
	for _, pattern := range r.Include {
		if match(pattern) {
			return true, "ref matches"
		}
	}
	return false, "ref does not match"
}
