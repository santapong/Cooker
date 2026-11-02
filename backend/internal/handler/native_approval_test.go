package handler

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/model"
)

func TestNativeAnnotatedTagApprovalRequiresExactObjectAndServerSubject(t *testing.T) {
	for _, kind := range []string{"exact", "missing-object", "different-object-same-commit"} {
		t.Run(kind, func(t *testing.T) {
			h, router, rule, source := nativeHandlerFixture(t)
			ctx := context.Background()
			cmd := exec.Command("git", "-C", source.root, "-c", "user.name=Owned Fixture", "-c", "user.email=fixture@example.invalid", "tag", "-a", "v1", "-m", "owned annotated tag")
			cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
			if err := cmd.Run(); err != nil {
				t.Fatal("owned tag fixture failed", err)
			}
			revision, err := source.Revision(ctx, nil, "", "refs/tags/v1")
			if err != nil || revision.Object == revision.Commit || revision.Commit != source.commit {
				t.Fatal("fixture did not produce annotated tag identity", err)
			}
			rule.RefKind, rule.Include, rule.Approval = "tag", []string{"v1"}, "review"
			preview, err := h.Automation.Preview(ctx, rule, revision.FullRef)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			preview.Rule.ReviewedBySubject, preview.Rule.ReviewedAt = "rule-reviewer", &now
			if err := h.Automation.Triggers.SaveRule(ctx, preview.Rule); err != nil {
				t.Fatal(err)
			}
			eventObject := revision.Object
			if kind == "missing-object" {
				eventObject = ""
			} else if kind == "different-object-same-commit" {
				eventObject = revision.Commit
			}
			delivery, _, err := h.Automation.Triggers.AcceptVerified(ctx, model.VerifiedGitEvent{Source: source.identity, DeliveryID: "owned-tag-delivery", Event: "push", FullRef: revision.FullRef, CommitSHA: revision.Commit, ObjectSHA: eventObject, PayloadHash: strings.Repeat("a", 64)})
			if err != nil || len(delivery.Decisions) != 1 || delivery.Decisions[0].IntentID == "" {
				t.Fatal("tag fixture did not produce review intent", err)
			}
			id := delivery.Decisions[0].IntentID
			before := source.reads
			if w := sendNativeRequest(t, router, "/intents/"+id+"/approve", nil, ""); w.Code != 401 || source.reads != before {
				t.Fatal("unauthenticated tag reviewer reached source", w.Code)
			}
			w := sendNativeRequest(t, router, "/intents/"+id+"/approve", map[string]string{"reviewerSubject": "forged-owner", "objectSha": revision.Object}, "server-reviewer")
			want := 409
			if kind == "exact" {
				want = 202
			}
			if w.Code != want {
				t.Fatal("tag approval lost exact identity", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), source.root) || strings.Contains(w.Body.String(), "owned-key-") {
				t.Fatal("private source data escaped approval response")
			}
			intent, err := h.Automation.Triggers.Store.GetIntent(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if want == 202 {
				if intent.Review == nil || intent.Review.ReviewerSubject != "server-reviewer" || intent.Review.ObjectSHA != revision.Object || intent.Review.CommitSHA != revision.Commit || intent.Rule.ActorSubject != "cooker:trigger:"+rule.ID {
					t.Fatal("server authority or annotated identity lost")
				}
			} else if intent.Review != nil || intent.Status != model.TriggerAwaitingReview {
				t.Fatal("missing or different tag object received review")
			}
		})
	}
}
