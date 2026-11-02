package vercel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSafeDisplayURLRejectsCredentialAndLocalLinks(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com/?secret=value", "https://example.com/#secret", "https://127.1", "https://0x7f.0.0.1", "https://127.0.0.1", "https://app.internal", "https://app.localhost", "https://app.example", "https://app.test", "https://app.com:443", "https://bad..com"} {
		if got := SafeDisplayURL(raw, false); got != "" {
			t.Fatalf("unsafe display link accepted: %s", raw)
		}
	}
	for _, raw := range []string{"app.vercel.app", "https://app.vercel.app/path"} {
		if SafeDisplayURL(raw, false) == "" {
			t.Fatal("safe URL rejected")
		}
	}
	if SafeDisplayURL("https://vercel.com/team/app/deployment", true) == "" || SafeDisplayURL("https://evil.com/dashboard", true) != "" {
		t.Fatal("dashboard origin restriction failed")
	}
}
func TestVerifiedPublicationLookupSurvivesRecentReviewWindow(t *testing.T) {
	p, _, c := publisherFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insert := func(index int, status, deployment string, created time.Time) {
		t.Helper()
		op := Publication{ID: fmt.Sprintf("%s-catalog-%03d", c.ID, index), ConnectionID: c.ID, ConnectionVersion: c.Version, Status: status, AccountID: c.Identity.ID, AccountScope: c.Identity.Scope, ProjectID: "prj_catalog", DeploymentID: deployment, ArtifactSHA256: strings.Repeat("a", 64), Target: "production"}
		data, err := json.Marshal(op)
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.DB.ExecContext(ctx, `INSERT INTO vercel_publications(id,connection_id,connection_version,actor,status,receipt,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7)`, op.ID, c.ID, c.Version, "fixture", status, data, created)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert(0, "succeeded", "dpl_verified_old", now.Add(-time.Hour))
	insert(1, "needs_attention", "dpl_uncertain", now)
	insert(2, "succeeded", "dpl_other", now)
	for i := 3; i < 106; i++ {
		insert(i, "reviewed", "", now.Add(time.Duration(i)*time.Second))
	}
	recent, err := p.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range recent {
		if op.DeploymentID == "dpl_verified_old" {
			t.Fatal("test did not exceed recent window")
		}
	}
	got, err := p.VerifiedForDeployments(ctx, []string{"dpl_verified_old", "dpl_uncertain", "dpl_verified_old"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DeploymentID != "dpl_verified_old" || got[0].Status != "succeeded" || got[0].ProjectID != "prj_catalog" {
		t.Fatalf("verified exact lookup lost provenance: %+v", got)
	}
	// This read does not require a configured token/deploy API/queue.
	readonly := &Publisher{DB: p.DB}
	got, err = readonly.VerifiedForDeployments(ctx, []string{"dpl_verified_old"})
	if err != nil || len(got) != 1 {
		t.Fatal("durable proof coupled to execution availability")
	}
	if got, err = p.VerifiedForDeployments(ctx, nil); err != nil || len(got) != 0 {
		t.Fatal("empty request returned receipts")
	}
}
