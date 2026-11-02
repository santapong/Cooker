package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store"
)

type failedAppHistory struct {
	store.AppDeployStore
	err error
}

func (s failedAppHistory) ListByApp(context.Context, string, int) ([]*model.AppDeploy, error) {
	return nil, s.err
}
func (s failedAppHistory) Get(context.Context, string) (*model.AppDeploy, error) { return nil, s.err }

type failedHistoryApp struct {
	store.AppStore
	err error
}

func (s failedHistoryApp) Get(context.Context, string) (*model.App, error) { return nil, s.err }

func TestAppHistoryReadErrorsAreSafe(t *testing.T) {
	const canary = "owned-history-error-canary /tmp/private-history-checkout credential=synthetic"
	for _, path := range []string{"history", "rollback-list", "rollback-id", "history-app", "rollback-app"} {
		t.Run(path, func(t *testing.T) {
			h := newTestHandler(t)
			seedRollbackApp(t, h, model.DeployTargetKubernetes)
			h.AppDeployer = service.NewAppDeployer(service.NewExecutor(), "reg")
			if strings.HasSuffix(path, "-app") {
				h.Store.Apps = failedHistoryApp{h.Store.Apps, errors.New(canary)}
			} else {
				h.Store.AppDeploys = failedAppHistory{h.Store.AppDeploys, errors.New(canary)}
			}
			method, url, body := http.MethodGet, "/apps/app-1/deploys", ""
			if strings.HasPrefix(path, "rollback") {
				method, url = http.MethodPost, "/apps/app-1/rollback"
			}
			if path == "rollback-id" {
				body = `{"deployId":"record"}`
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(method, url, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rollbackRouter(h).ServeHTTP(w, req)
			if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), canary) || strings.Contains(w.Body.String(), "private-history-checkout") {
				t.Fatalf("history error escaped safe boundary: status%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAppHistoryReadErrorsRetainKnownStatuses(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{store.ErrNotFound, http.StatusNotFound}, {store.ErrConflict, http.StatusConflict}} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			h := newTestHandler(t)
			seedRollbackApp(t, h, model.DeployTargetKubernetes)
			h.AppDeployer = service.NewAppDeployer(service.NewExecutor(), "reg")
			h.Store.AppDeploys = failedAppHistory{h.Store.AppDeploys, tc.err}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/apps/app-1/rollback", strings.NewReader(`{"deployId":"record"}`))
			req.Header.Set("Content-Type", "application/json")
			rollbackRouter(h).ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("known history error changed status: %d", w.Code)
			}
		})
	}
}
