package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

type admissionQueueFailure struct{}

func (admissionQueueFailure) EnqueueRun(context.Context, string, string) error {
	return errors.New("queue offline")
}
func (admissionQueueFailure) EnqueuePlannedRun(context.Context, *model.Pipeline, *model.PipelineRun) error {
	return errors.New("queue offline")
}

type admissionRunViewFailure struct{ store.RunStore }

func (s admissionRunViewFailure) Create(context.Context, *model.PipelineRun) error {
	return errors.New("run view temporarily offline")
}

func TestRunPipelineFailedAdmissionDoesNotLeavePendingRun(t *testing.T) {
	h := newTestHandler(t)
	h.Enqueuer = admissionQueueFailure{}
	p := &model.Pipeline{ID: "p", Name: "pipeline"}
	if err := h.Store.Pipelines.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/pipelines/:id/run", h.RunPipeline)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines/p/run", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	runs, err := h.Store.Runs.List(context.Background(), p.ID, 100, 0)
	if err != nil || len(runs) != 0 {
		t.Fatalf("unaccepted pending work: %+v %v", runs, err)
	}
}

func TestRunPipelineAcceptedBeforeRunViewAndWorkerRecovers(t *testing.T) {
	ctx := context.Background()
	h := newTestHandler(t)
	jobs := jobqueue.NewMemory()
	p := &model.Pipeline{ID: "p", Name: "pipeline", Stages: []model.Stage{{ID: "build", Type: model.StageTypeBuild}}}
	if err := h.Store.Pipelines.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	h.Enqueuer = service.NewJobQueueEnqueuer(jobs, h.Store.Pipelines)
	actualRuns := h.Store.Runs
	h.Store.Runs = admissionRunViewFailure{actualRuns}
	router := gin.New()
	router.POST("/pipelines/:id/run", withUser(h.RunPipeline, &auth.Claims{Subject: "caller", Email: "caller@example.test", Groups: []string{"deployers"}}))
	w := httptest.NewRecorder()
	request := newRequest(http.MethodPost, "/pipelines/p/run", nil)
	request.Header.Set("Authorization", "Bearer admission-test-token")
	router.ServeHTTP(w, request)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var accepted model.PipelineRun
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Get(ctx, "run:"+accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := actualRuns.Get(ctx, accepted.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unexpected view before recovery: %v", err)
	}
	h.Store.Runs = actualRuns
	if err := service.NewJobQueueRunner(h.Store, service.NewExecutor(), nil).Handle(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := actualRuns.Get(ctx, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.RunStatusSuccess || got.StartedByUserSub != "caller" || got.StartedByEmail != "caller@example.test" || len(got.StartedByGroups) != 1 || got.StartedByGroups[0] != "deployers" || got.StartedByTokenHash == "" || got.StartedByTokenHash == "admission-test-token" {
		t.Fatalf("accepted actor not recovered: %+v", got)
	}
}
