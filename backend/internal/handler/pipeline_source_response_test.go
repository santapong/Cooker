package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
)

func TestRunPipelineBoundSourceNeedsQueueBeforeCreatingRun(t *testing.T) {
	h := newTestHandler(t)
	p := &model.Pipeline{ID: "owned-source", Name: "owned", Source: &model.PipelineSourceConfig{ConnectionID: "owned", Repository: "owned/repo", FullRef: "refs/heads/main"}}
	if err := h.Store.Pipelines.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/pipelines/:id/run", h.RunPipeline)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/owned-source/run", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal(w.Code, w.Body.String())
	}
	runs, err := h.Store.Runs.List(context.Background(), p.ID, 10, 0)
	if err != nil || len(runs) != 0 {
		t.Fatal("unaccepted source run was created")
	}
}

type sourceResponseQueue struct {
	accepted                   *model.PipelineRun
	source                     *model.PipelineRunSource
	unavailable, wrongIdentity bool
	reads                      int
}

func (*sourceResponseQueue) EnqueueRun(context.Context, string, string) error {
	return errors.New("legacy path must not be used")
}
func (q *sourceResponseQueue) EnqueuePlannedRun(_ context.Context, _ *model.Pipeline, run *model.PipelineRun) error {
	q.accepted = run.Clone()
	q.accepted.Source = q.source.Clone()
	return nil
}
func (q *sourceResponseQueue) AcceptedRunSnapshot(context.Context, string) (*model.PipelineRun, error) {
	q.reads++
	if q.unavailable {
		return nil, errors.New("query unavailable")
	}
	out := q.accepted.Clone()
	if q.wrongIdentity {
		out.ID = "different-run"
	}
	return out, nil
}

func TestRunPipelineAcceptedSourceProjectionAndUnavailableQuery(t *testing.T) {
	for _, name := range []string{"accepted-source", "query-unavailable", "wrong-identity"} {
		t.Run(name, func(t *testing.T) {
			h := newTestHandler(t)
			p := &model.Pipeline{ID: "owned-source", Name: "owned", Source: &model.PipelineSourceConfig{ConnectionID: "owned", Repository: "owned/repo", FullRef: "refs/heads/main"}}
			if err := h.Store.Pipelines.Create(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			q := &sourceResponseQueue{source: &model.PipelineRunSource{Source: model.GitSourceIdentity{Provider: "github", ConnectionID: "owned", Host: "github.com", RepositoryID: "42", Repository: "owned/repo"}, FullRef: "refs/heads/main", CommitSHA: strings.Repeat("a", 40), ObjectSHA: strings.Repeat("a", 40), SourcePlanDigest: strings.Repeat("b", 64)}, unavailable: name == "query-unavailable", wrongIdentity: name == "wrong-identity"}
			h.Enqueuer = q
			router := gin.New()
			router.POST("/pipelines/:id/run", withUser(h.RunPipeline, &auth.Claims{Subject: "owned-actor", Groups: []string{"operators"}}))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/owned-source/run", nil))
			if w.Code != http.StatusAccepted {
				t.Fatal(w.Code, w.Body.String())
			}
			var returned model.PipelineRun
			if err := json.Unmarshal(w.Body.Bytes(), &returned); err != nil {
				t.Fatal(err)
			}
			stored, err := h.Store.Runs.Get(context.Background(), returned.ID)
			if err != nil {
				t.Fatal(err)
			}
			if q.reads != 1 || returned.StartedByUserSub != "owned-actor" || returned.PipelineVersion != p.Version {
				t.Fatal("wrong accepted identity")
			}
			if name == "accepted-source" {
				if returned.Source == nil || stored.Source == nil || returned.Source.CommitSHA != q.source.CommitSHA || returned.Source.CheckedOutAt != nil {
					t.Fatal("receipt missing or falsely claimed checkout")
				}
				returned.Source.CommitSHA = strings.Repeat("c", 40)
				if q.accepted.Source.CommitSHA != q.source.CommitSHA {
					t.Fatal("query aliases admission")
				}
			} else if returned.Source != nil || stored.Source != nil {
				t.Fatal("unavailable/mismatched query manufactured source")
			}
		})
	}
}
