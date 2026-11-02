package model

import "testing"

func TestNativeTriggerMatchFailsClosedForInvalidPolicy(t *testing.T) {
	identity := GitSourceIdentity{Provider: "github", ConnectionID: "approved", Host: "github.com", RepositoryID: "42", Repository: "owned/native"}
	base := TriggerRule{Enabled: true, Source: identity, RefKind: "branch", Match: "exact", Include: []string{"main"}}
	event := VerifiedGitEvent{Source: identity, FullRef: "refs/heads/main"}
	if ok, _ := base.Matches(event); !ok {
		t.Fatal("valid exact rule was rejected")
	}
	for _, kind := range []string{"kind", "matcher", "empty-ref", "excluded", "other-repository", "tag-namespace"} {
		t.Run(kind, func(t *testing.T) {
			rule, delivery := base, event
			switch kind {
			case "kind":
				rule.RefKind = "unknown"
			case "matcher":
				rule.Match = "unknown"
			case "empty-ref":
				rule.Include = []string{"*"}
				rule.Match = "glob"
				delivery.FullRef = "refs/heads/"
			case "excluded":
				rule.Exclude = []string{"main"}
			case "other-repository":
				delivery.Source.RepositoryID = "other"
			case "tag-namespace":
				delivery.FullRef = "refs/tags/main"
			}
			if ok, _ := rule.Matches(delivery); ok {
				t.Fatal("unsafe rule/event accepted")
			}
		})
	}
}
