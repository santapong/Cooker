package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegistryGrantsCASValidationAndCredentialRedaction(t *testing.T) {
	h := withConfigServices(newTestHandler(t))
	r := gin.New()
	r.POST("/registries", h.AddRegistryConfig)
	r.PATCH("/registries/:id/grants", h.UpdateRegistryGrants)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/registries", map[string]any{"name": "fixture", "url": "ghcr.io", "password": "owned-registry-grant-canary", "allowedAppIds": []string{"app"}}))
	if w.Code != 201 || strings.Contains(w.Body.String(), "canary") {
		t.Fatal("create grants or redaction failed")
	}
	var config map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &config)
	id, _ := config["id"].(string)
	version := config["version"]
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPatch, "/registries/"+id+"/grants", map[string]any{"version": version, "allowedPipelineIds": []string{"pipeline"}}))
	if w.Code != 200 || strings.Contains(w.Body.String(), "canary") {
		t.Fatal("grant update/redaction failed")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPatch, "/registries/"+id+"/grants", map[string]any{"version": version, "allowedAppIds": []string{"stale"}}))
	if w.Code != 409 {
		t.Fatal("stale admin page restored grants")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPatch, "/registries/"+id+"/grants", map[string]any{"version": version, "allowedAppIds": []string{"../SECRET_CANARY"}}))
	if w.Code != 400 || strings.Contains(w.Body.String(), "SECRET_CANARY") {
		t.Fatal("invalid grants echoed or accepted")
	}
}
