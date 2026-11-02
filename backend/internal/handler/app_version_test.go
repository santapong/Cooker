package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
)

func TestAppCreatedVersionSupportsFirstConditionalEdit(t *testing.T) {
	for _, initial := range []int{0, 99} {
		name := "omitted-default"
		if initial != 0 {
			name = "caller-supplied"
		}
		t.Run(name, func(t *testing.T) {
			h := newTestHandler(t)
			admin := &auth.Claims{Roles: []string{string(auth.RoleAdmin)}}
			r := gin.New()
			r.POST("/apps", withUser(h.CreateApp, admin))
			r.GET("/apps/:id", withUser(h.GetApp, admin))
			r.PUT("/apps/:id", withUser(h.UpdateApp, admin))
			input := model.App{Name: "versioned-app", GitHubRepo: "fixture/web", Branch: "main", Version: initial}
			created := httptest.NewRecorder()
			r.ServeHTTP(created, newRequest(http.MethodPost, "/apps", input))
			if created.Code != http.StatusCreated {
				t.Fatalf("create status: %d", created.Code)
			}
			var app model.App
			if json.Unmarshal(created.Body.Bytes(), &app) != nil || app.ID == "" || app.Version != 1 {
				t.Fatal("create must return server-owned initial version one")
			}
			read := httptest.NewRecorder()
			r.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/apps/"+app.ID, nil))
			var persisted model.App
			if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &persisted) != nil || persisted.Version != app.Version {
				t.Fatal("create response and persisted version must match")
			}
			app.Description = "first accepted edit"
			edited := httptest.NewRecorder()
			r.ServeHTTP(edited, newRequest(http.MethodPut, "/apps/"+app.ID, app))
			var result model.App
			if edited.Code != http.StatusOK || json.Unmarshal(edited.Body.Bytes(), &result) != nil || result.Version != 2 {
				t.Fatal("create response must authorize the first conditional edit")
			}
			app.Description = "stale overwrite"
			stale := httptest.NewRecorder()
			r.ServeHTTP(stale, newRequest(http.MethodPut, "/apps/"+app.ID, app))
			if stale.Code != http.StatusConflict {
				t.Fatalf("stale edit status: %d", stale.Code)
			}
			stored, err := h.Store.Apps.Get(t.Context(), app.ID)
			if err != nil || stored.Version != 2 || stored.Description != result.Description {
				t.Fatal("stale edit must not change the committed definition")
			}
		})
	}
}
