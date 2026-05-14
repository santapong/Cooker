package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/notifier"
	"github.com/santapong/cooker/internal/scheduler"
	"github.com/santapong/cooker/internal/templates"
)

// --- /admin/templates ---

func TestAdminTemplates_CreateGetUpdateDelete(t *testing.T) {
	h := newTestHandler(t)
	h.Templates = templates.NewMemoryStore()

	r := gin.New()
	r.POST("/admin/templates", h.CreateTemplate)
	r.GET("/admin/templates/:id", h.GetTemplate)
	r.PUT("/admin/templates/:id", h.UpdateTemplate)
	r.DELETE("/admin/templates/:id", h.DeleteTemplate)

	body := map[string]any{
		"name":        "web-build",
		"description": "Build + push a web image",
		"category":    "ci",
		"schema":      json.RawMessage(`{"stages":[]}`),
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/admin/templates", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created templates.Template
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || !created.Enabled {
		t.Fatalf("default fields not populated: %+v", created)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/admin/templates/"+created.ID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}

	disabled := false
	body["enabled"] = &disabled
	body["name"] = "web-build-v2"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/admin/templates/"+created.ID, body))
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var updated templates.Template
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Name != "web-build-v2" || updated.Enabled {
		t.Fatalf("update did not apply: %+v", updated)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/admin/templates/"+created.ID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/admin/templates/"+created.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d", w.Code)
	}
}

func TestAdminTemplates_503WhenNotConfigured(t *testing.T) {
	h := newTestHandler(t) // Templates stays nil
	r := gin.New()
	r.POST("/admin/templates", h.CreateTemplate)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/admin/templates", map[string]any{
		"name":   "x",
		"schema": json.RawMessage(`{}`),
	}))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 got %d", w.Code)
	}
}

// --- /admin/schedules ---

func TestAdminSchedules_CreateValidatesCron(t *testing.T) {
	h := newTestHandler(t)
	h.Schedules = scheduler.NewMemoryStore()

	r := gin.New()
	r.POST("/admin/schedules", h.CreateSchedule)

	bad := map[string]any{
		"pipelineId": "00000000-0000-0000-0000-000000000000",
		"cronExpr":   "not a cron",
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/admin/schedules", bad))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad cron: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminSchedules_CreateGetListUpdateDelete(t *testing.T) {
	h := newTestHandler(t)
	h.Schedules = scheduler.NewMemoryStore()

	r := gin.New()
	r.POST("/admin/schedules", h.CreateSchedule)
	r.GET("/admin/schedules", h.ListSchedules)
	r.GET("/admin/schedules/:id", h.GetSchedule)
	r.PUT("/admin/schedules/:id", h.UpdateSchedule)
	r.DELETE("/admin/schedules/:id", h.DeleteSchedule)

	body := map[string]any{
		"pipelineId": "00000000-0000-0000-0000-000000000001",
		"name":       "nightly",
		"cronExpr":   "0 2 * * *",
		"timezone":   "UTC",
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/admin/schedules", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created scheduler.Schedule
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.NextRunAt.Before(time.Now()) {
		t.Fatalf("nextRunAt should be in the future, got %v", created.NextRunAt)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/admin/schedules", nil))
	var list []scheduler.Schedule
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(list))
	}

	body["cronExpr"] = "*/5 * * * *"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/admin/schedules/"+created.ID, body))
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/admin/schedules/"+created.ID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/admin/schedules/"+created.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d", w.Code)
	}
}

// --- /admin/notification-targets ---

func TestAdminNotificationTargets_RejectsUnknownKind(t *testing.T) {
	h := newTestHandler(t)
	h.NotificationTargets = notifier.NewMemoryTargetStore()

	r := gin.New()
	r.POST("/admin/notification-targets", h.CreateNotificationTarget)

	body := map[string]any{
		"name":   "tg-bot",
		"kind":   "telegram", // not yet supported
		"config": json.RawMessage(`{"token":"x"}`),
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/admin/notification-targets", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown kind, got %d", w.Code)
	}
}

func TestAdminNotificationTargets_CRUD(t *testing.T) {
	h := newTestHandler(t)
	h.NotificationTargets = notifier.NewMemoryTargetStore()

	r := gin.New()
	r.POST("/admin/notification-targets", h.CreateNotificationTarget)
	r.GET("/admin/notification-targets", h.ListNotificationTargets)
	r.GET("/admin/notification-targets/:id", h.GetNotificationTarget)
	r.PUT("/admin/notification-targets/:id", h.UpdateNotificationTarget)
	r.DELETE("/admin/notification-targets/:id", h.DeleteNotificationTarget)

	body := map[string]any{
		"name":       "deploy-slack",
		"kind":       "slack",
		"config":     json.RawMessage(`{"webhookUrl":"https://hooks.slack.test/abc"}`),
		"eventTypes": []string{"deploy.succeeded", "deploy.failed"},
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/admin/notification-targets", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created notifier.Target
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if !created.Enabled || created.Kind != "slack" {
		t.Fatalf("defaults wrong: %+v", created)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/admin/notification-targets", nil))
	var list []notifier.Target
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("expected 1 target, got %d", len(list))
	}

	disabled := false
	body["enabled"] = &disabled
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPut, "/admin/notification-targets/"+created.ID, body))
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodDelete, "/admin/notification-targets/"+created.ID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
}
