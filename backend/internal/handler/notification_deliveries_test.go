package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/notify/notifier"
)

type deliveryFixture struct {
	query notifier.DeliveryQuery
	err   error
}

func (s *deliveryFixture) ListDeliveries(_ context.Context, q notifier.DeliveryQuery) (notifier.DeliveryPage, error) {
	s.query = q
	return notifier.DeliveryPage{Items: []notifier.Delivery{{ID: "delivery-one", EventID: "event-one", TargetID: "target-one", State: "needs_attention", LastCode: "worker_lost", DuplicatePossible: true}}, Total: 1, Mode: "Durable; uncertain sends require inspection", PendingSourceJobs: 3, PendingSourceRepairJobs: 2}, s.err
}
func (s *deliveryFixture) GetDelivery(context.Context, string) (notifier.Delivery, error) {
	return notifier.Delivery{}, s.err
}

func TestNotificationDeliveryHistorySafeShapeFiltersAndErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := &deliveryFixture{}
	h := &Handler{NotificationDeliveries: fixture}
	r := gin.New()
	r.GET("/deliveries", h.ListNotificationDeliveries)
	r.GET("/deliveries/:id", h.GetNotificationDelivery)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/deliveries?state=needs_attention&targetId=target-one&runId=run-one&limit=1&offset=2", nil))
	if w.Code != 200 || fixture.query.State != "needs_attention" || fixture.query.Limit != 1 || fixture.query.Offset != 2 {
		t.Fatal("history filters not propagated")
	}
	for _, field := range []string{"\"config\"", "\"payload\"", "\"leaseOwner\"", "\"error\""} {
		if strings.Contains(w.Body.String(), field) {
			t.Fatal("history exposed a private delivery field")
		}
	}
	if !strings.Contains(w.Body.String(), "pendingSourceRepairJobs") || !strings.Contains(w.Body.String(), "duplicatePossible") {
		t.Fatal("history hid uncertainty/backlog")
	}
	fixture.err = errors.New("fixture-secret-provider-body")
	for _, path := range []string{"/deliveries", "/deliveries/one"} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 503 || strings.Contains(w.Body.String(), "fixture-secret") {
			t.Fatal("backend error leaked")
		}
	}
	fixture.err = notifier.ErrDeliveryNotFound
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/deliveries/absent", nil))
	if w.Code != 404 {
		t.Fatal("missing delivery status incorrect")
	}
	h.NotificationDeliveries = nil
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/deliveries", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "best-effort") {
		t.Fatal("memory mode claimed durability")
	}
}
