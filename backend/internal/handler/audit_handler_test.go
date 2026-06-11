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

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type auditListResponse struct {
	Events []*model.AuditEvent `json:"events"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

func auditTestRouter(h *Handler) *gin.Engine {
	r := gin.New()
	r.GET("/admin/audit", h.ListAuditEvents)
	return r
}

func TestListAuditEvents_StoreNotConfigured(t *testing.T) {
	h := &Handler{Store: store.New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)}
	w := httptest.NewRecorder()
	auditTestRouter(h).ServeHTTP(w, newRequest(http.MethodGet, "/admin/audit", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
}

func TestListAuditEvents_BadParams(t *testing.T) {
	h := newTestHandler(t)
	r := auditTestRouter(h)
	for _, target := range []string{
		"/admin/audit?from=yesterday",
		"/admin/audit?to=2026-06-01", // date only, not RFC3339
		"/admin/audit?limit=0",
		"/admin/audit?limit=abc",
		"/admin/audit?offset=-1",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", target, w.Code)
		}
	}
}

func TestListAuditEvents_FiltersAndPaging(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		sub := "alice"
		if i%2 == 1 {
			sub = "bob"
		}
		if err := h.Store.AuditEvents.Insert(ctx, &model.AuditEvent{
			Time: base.Add(time.Duration(i) * time.Minute), UserSub: sub,
			Method: "POST", Path: fmt.Sprintf("/api/v1/pipelines/p%d", i), Status: 201,
		}); err != nil {
			t.Fatal(err)
		}
	}
	r := auditTestRouter(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/admin/audit?user=alice", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var resp auditListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) != 3 || resp.Limit != 50 || resp.Offset != 0 {
		t.Fatalf("user filter: %d events limit=%d offset=%d", len(resp.Events), resp.Limit, resp.Offset)
	}
	for _, e := range resp.Events {
		if e.UserSub != "alice" {
			t.Fatalf("leaked event for %q", e.UserSub)
		}
	}

	// RFC3339 window + paging echo.
	from := base.Add(90 * time.Second).Format(time.RFC3339)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/admin/audit?from="+from+"&limit=2&offset=1", nil))
	resp = auditListResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) != 2 || resp.Limit != 2 || resp.Offset != 1 {
		t.Fatalf("window+paging: %d events limit=%d offset=%d", len(resp.Events), resp.Limit, resp.Offset)
	}
	// Newest-first within the window (events at +2,+3,+4 min; offset 1
	// skips +4) — first page entry is +3min.
	if !resp.Events[0].Time.Equal(base.Add(3 * time.Minute)) {
		t.Fatalf("expected event at +3min first, got %v", resp.Events[0].Time)
	}

	// Limit clamps to the max instead of erroring.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/admin/audit?limit=9999", nil))
	resp = auditListResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Limit != auditListMaxLimit {
		t.Fatalf("limit not clamped: %d", resp.Limit)
	}

	// Empty result is [] not null.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/admin/audit?user=mallory", nil))
	if body := w.Body.String(); !json.Valid([]byte(body)) || resp.Events == nil {
		t.Fatalf("expected valid JSON with empty array, got %s", body)
	}
}
