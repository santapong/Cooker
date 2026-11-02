package service

import (
	"context"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/jobqueue"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestScheduledAdmissionRequiresAtomicQueueAndSnapshot(t *testing.T) {
	pipelines := memory.New().Pipelines
	if err := pipelines.Create(context.Background(), &model.Pipeline{ID: "p"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	occurrence := ScheduledOccurrence{ScheduleID: "s", ScheduleVersion: 1, PipelineID: "p", ScheduledFor: now.Add(-time.Minute), AdmittedAt: now, NextRunAt: now.Add(time.Minute)}
	for _, enq := range []*JobQueueEnqueuer{nil, NewJobQueueEnqueuer(jobqueue.NewMemory()), NewJobQueueEnqueuer(jobqueue.NewMemory(), pipelines)} {
		if err := enq.EnqueueScheduledRun(context.Background(), occurrence); err == nil {
			t.Fatal("unconfigured/non-atomic scheduler admitted work")
		}
	}
}
