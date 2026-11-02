package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/store/memory"
)

type nativeHandlerSource struct {
	root, commit string
	reads        int
	identity     model.GitSourceIdentity
}

func (s *nativeHandlerSource) Identity(context.Context, *connection.Connection, string) (model.GitSourceIdentity, error) {
	s.reads++
	return s.identity, nil
}
func (s *nativeHandlerSource) Revision(ctx context.Context, _ *connection.Connection, _ string, ref string) (service.SourceRevision, error) {
	s.reads++
	if strings.HasPrefix(ref, "refs/tags/") {
		objectCmd := exec.CommandContext(ctx, "git", "-C", s.root, "rev-parse", ref)
		objectCmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		object, err := objectCmd.Output()
		if err != nil {
			return service.SourceRevision{}, err
		}
		commitCmd := exec.CommandContext(ctx, "git", "-C", s.root, "rev-parse", ref+"^{commit}")
		commitCmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		commit, err := commitCmd.Output()
		if err != nil {
			return service.SourceRevision{}, err
		}
		return service.SourceRevision{FullRef: ref, Commit: strings.TrimSpace(string(commit)), Object: strings.TrimSpace(string(object))}, nil
	}
	return service.SourceRevision{FullRef: ref, Commit: s.commit, Object: s.commit}, nil
}
func (s *nativeHandlerSource) CheckoutSource(ctx context.Context, plan *model.PipelineSourcePlan, _ io.Writer) (string, error) {
	s.reads++
	root, err := os.MkdirTemp("", "cooker-owned-native-handler-")
	if err != nil {
		return "", err
	}
	clone := exec.CommandContext(ctx, "git", "clone", "--no-hardlinks", s.root, root)
	clone.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if err = clone.Run(); err != nil {
		os.RemoveAll(root)
		return "", err
	}
	checkout := exec.CommandContext(ctx, "git", "-C", root, "checkout", "--detach", plan.CommitSHA)
	checkout.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if err = checkout.Run(); err != nil {
		os.RemoveAll(root)
		return "", err
	}
	return root, nil
}
func nativeHandlerFixture(t *testing.T) (*Handler, *gin.Engine, *model.TriggerRule, *nativeHandlerSource) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	root := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal("owned git fixture command failed", err)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "Dockerfile")
	runGit("-c", "user.name=Owned Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "owned fixture")
	codec, _ := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	cs := &connection.Service{Store: connection.NewMemory(), Codec: codec}
	conn := &connection.Connection{Name: "owned", Provider: "github", Enabled: true, Repositories: []string{"owned/native"}}
	if err := cs.Create(ctx, conn, strings.Repeat("owned-key-", 4)); err != nil {
		t.Fatal(err)
	}
	source := &nativeHandlerSource{root: root, commit: runGit("rev-parse", "HEAD"), identity: model.GitSourceIdentity{ConnectionID: conn.ID, Provider: "github", Host: "github.com", RepositoryID: "owned-repository", Repository: "owned/native"}}
	st := memory.New()
	p := &model.Pipeline{ID: "owned-native", Name: "Owned Native", Source: &model.PipelineSourceConfig{ConnectionID: conn.ID, Repository: "owned/native", FullRef: "refs/heads/main"}, Stages: []model.Stage{{ID: "build", Type: model.StageTypeBuild, Config: model.StageConfig{Context: ".", Dockerfile: "Dockerfile", Image: "owned/native:v1"}}}}
	p.SetDefaults()
	if err := st.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	triggers := &service.TriggerService{Store: memory.NewTriggerStore()}
	native := &service.PipelineAutomation{Triggers: triggers, Preparer: &service.PipelineSourcePreparer{Store: st, Connections: cs, Source: source}, Authorize: func(_ context.Context, r *model.TriggerRule, p *model.Pipeline) error {
		if r.ActorSubject != "cooker:trigger:"+r.ID || len(r.ActorGroups) != 0 || p.ID != "owned-native" {
			t.Fatal("forged actor or target reached authorization")
		}
		return nil
	}}
	h := &Handler{Store: st, Automation: &service.Automation{Triggers: triggers, Native: native}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if subject := c.GetHeader("X-Owned-Reviewer"); subject != "" {
			c.Set("user", &auth.Claims{Subject: subject, Roles: []string{"admin"}})
		}
		c.Next()
	})
	router.POST("/preview", h.PreviewTriggerRule)
	router.POST("/save", h.SaveTriggerRule)
	router.POST("/intents/:id/approve", h.ApproveTriggerIntent)
	router.POST("/rules/:id/check", h.CheckTriggerRef)
	rule := &model.TriggerRule{ID: uuid.NewString(), Name: "Owned branch", Enabled: true, TargetKind: "pipeline", TargetID: p.ID, Source: source.identity, RefKind: "branch", Match: "exact", Include: []string{"main"}, Approval: "automatic"}
	return h, router, rule, source
}
func sendNativeRequest(t *testing.T, router *gin.Engine, path string, v any, reviewer string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owned-Reviewer", reviewer)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestNativeRuleSaveDerivesSourceAndAuthenticatedReviewer(t *testing.T) {
	h, router, rule, source := nativeHandlerFixture(t)
	forged := time.Now().Add(-time.Hour)
	rule.ReviewedBySubject = "forged-reviewer"
	rule.ReviewedAt = &forged
	rule.ActorSubject = "forged-admin"
	rule.ActorGroups = []string{"admin"}
	rule.PipelineVersion = 900
	rule.SourceVersion = 900
	request := triggerRequest{Rule: *rule, Ref: "refs/heads/main"}
	before := source.reads
	if w := sendNativeRequest(t, router, "/save", request, ""); w.Code != 401 || source.reads != before {
		t.Fatal("missing reviewer reached source", w.Code)
	}
	w := sendNativeRequest(t, router, "/preview", request, "administrator")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var preview service.TriggerPreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Rule.ReviewedAt != nil || preview.Rule.ReviewedBySubject != "" || preview.Rule.ActorSubject != "cooker:trigger:"+rule.ID || len(preview.Rule.ActorGroups) != 0 || preview.Rule.PipelineVersion == 900 || preview.Rule.SourceVersion == 900 {
		t.Fatal("forged authority survived preview")
	}
	if w := sendNativeRequest(t, router, "/save", request, "administrator"); w.Code != 409 {
		t.Fatal("unreviewed source/configuration accepted", w.Code)
	}
	request.ExpectedPlanHash = preview.Rule.PlanHash
	request.ExpectedEnvelopeHash = preview.Rule.ApprovedEnvelopeHash
	w = sendNativeRequest(t, router, "/save", request, "administrator")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved, err := h.Automation.Triggers.Store.GetRule(context.Background(), rule.ID)
	if err != nil || saved.ReviewedBySubject != "administrator" || saved.ReviewedAt == nil || !saved.ReviewedAt.After(forged) || saved.ActorSubject != "cooker:trigger:"+rule.ID {
		t.Fatal("trusted review not persisted", err)
	}
	if strings.Contains(w.Body.String(), source.root) || strings.Contains(w.Body.String(), "owned-key-") {
		t.Fatal("private source detail exposed")
	}
	before = source.reads
	request.Rule.TargetID = "vflow_imported"
	if w := sendNativeRequest(t, router, "/preview", request, "administrator"); w.Code != 422 || source.reads != before {
		t.Fatal("imported flow gained native source access", w.Code)
	}
}

func TestNativeExactIntentReviewUsesNativeServiceAndServerSubject(t *testing.T) {
	h, router, rule, source := nativeHandlerFixture(t)
	rule.Approval = "review"
	ctx := context.Background()
	preview, err := h.Automation.Preview(ctx, rule, "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	preview.Rule.ReviewedBySubject = "rule-reviewer"
	preview.Rule.ReviewedAt = &now
	if err := h.Automation.Triggers.SaveRule(ctx, preview.Rule); err != nil {
		t.Fatal(err)
	}
	delivery, _, err := h.Automation.Triggers.AcceptVerified(ctx, model.VerifiedGitEvent{Source: source.identity, DeliveryID: "owned-delivery", Event: "push", FullRef: "refs/heads/main", CommitSHA: source.commit, PayloadHash: strings.Repeat("a", 64)})
	if err != nil || len(delivery.Decisions) != 1 {
		t.Fatal("owned event not accepted", err)
	}
	id := delivery.Decisions[0].IntentID
	before := source.reads
	if w := sendNativeRequest(t, router, "/intents/"+id+"/approve", nil, ""); w.Code != 401 || source.reads != before {
		t.Fatal("unauthenticated intent approval reached source", w.Code)
	}
	if w := sendNativeRequest(t, router, "/intents/"+id+"/approve", nil, "intent-reviewer"); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	intent, err := h.Automation.Triggers.Store.GetIntent(ctx, id)
	if err != nil || intent.Review == nil || intent.Review.ReviewerSubject != "intent-reviewer" || intent.Review.CommitSHA != source.commit || intent.Review.ObjectSHA != source.commit || intent.Event.ObjectSHA != "" || intent.Rule.ActorSubject != "cooker:trigger:"+rule.ID {
		t.Fatal("native exact review lost actor/revision", err)
	}
	if w := sendNativeRequest(t, router, "/intents/"+id+"/approve", nil, "replacement-reviewer"); w.Code != 409 {
		t.Fatal("stale review replaced exact receipt", w.Code)
	}
	if w := sendNativeRequest(t, router, "/rules/"+rule.ID+"/check", map[string]string{"ref": "refs/heads/main"}, "administrator"); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := sendNativeRequest(t, router, "/rules/"+rule.ID+"/check", map[string]string{"ref": "refs/heads/other"}, "administrator"); w.Code != 422 {
		t.Fatal("unreviewed ref repair accepted", w.Code)
	}
}
