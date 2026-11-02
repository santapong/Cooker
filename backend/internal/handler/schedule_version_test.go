package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/scheduler"
)

func TestScheduleUpdateClientVersionAndLegacyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := scheduler.NewMemoryStore()
	if err := st.Create(context.Background(), scheduler.Schedule{ID: "s", PipelineID: "00000000-0000-0000-0000-000000000001", CronExpr: "* * * * *", Timezone: "UTC", Enabled: true, NextRunAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Schedules: st}
	r := gin.New()
	r.PUT("/schedules/:id", h.UpdateSchedule)
	for _, tc := range []struct {
		version     string
		status      int
		wantVersion int
	}{{`,"version":99`, 409, 1}, {`,"version":1`, 200, 2}, {`,"version":1`, 409, 2}, {``, 200, 3}} {
		body := []byte(fmt.Sprintf(`{"pipelineId":"00000000-0000-0000-0000-000000000001","cronExpr":"* * * * *","enabled":false%s}`, tc.version))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPut, "/schedules/s", json.RawMessage(body)))
		if w.Code != tc.status {
			t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
		}
		current, _ := st.Get(context.Background(), "s")
		if current.Version != tc.wantVersion {
			t.Fatalf("version=%d want=%d", current.Version, tc.wantVersion)
		}
		if w.Code == 200 {
			var returned scheduler.Schedule
			if err := json.Unmarshal(w.Body.Bytes(), &returned); err != nil || returned.Version != current.Version {
				t.Fatalf("returned version=%d current=%d err=%v", returned.Version, current.Version, err)
			}
		}
	}
}

type firedDuringEdit struct{ *scheduler.MemoryStore }

func (s firedDuringEdit) Update(ctx context.Context, schedule scheduler.Schedule) error {
	if err := s.MarkFired(ctx, schedule.ID, time.Now(), "concurrent-run", time.Now().Add(time.Hour)); err != nil {
		return err
	}
	return s.MemoryStore.Update(ctx, schedule)
}

func TestScheduleUpdateFireConflictReturns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := scheduler.NewMemoryStore()
	_ = st.Create(context.Background(), scheduler.Schedule{ID: "s", PipelineID: "00000000-0000-0000-0000-000000000001", CronExpr: "* * * * *", Enabled: true, NextRunAt: time.Now()})
	h := &Handler{Schedules: firedDuringEdit{MemoryStore: st}}
	r := gin.New()
	r.PUT("/schedules/:id", h.UpdateSchedule)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/schedules/s", json.RawMessage(`{"pipelineId":"00000000-0000-0000-0000-000000000001","cronExpr":"* * * * *","enabled":false}`)))
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	current, _ := st.Get(context.Background(), "s")
	if !current.Enabled || current.Version != 2 || current.LastRunID != "concurrent-run" {
		t.Fatalf("edit overwrote fire: %+v", current)
	}
}
