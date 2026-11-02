package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/model"
)

func TestPipelineExplicitVersionRefusesStaleAndInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		status      int
	}{
		{"stale", `,"version":0`, 409},
		{"future", `,"version":999`, 409},
		{"null", `,"version":null`, 400},
		{"negative", `,"version":-1`, 400},
		{"string", `,"version":"1"`, 400},
		{"overflow", `,"version":999999999999999999999999`, 400},
		{"current", `,"version":1`, 200},
		{"legacy omitted", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(t)
			if h.Store.Pipelines.Create(context.Background(), &model.Pipeline{ID: "p", Name: "original", Version: 1}) != nil {
				t.Fatal("fixture create failed")
			}
			r := gin.New()
			r.PUT("/pipelines/:id", h.UpdatePipeline)
			body := `{"name":"edited","stages":[],"edges":[],"variables":{}` + tc.field + `}`
			w := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/pipelines/p", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, request)
			if w.Code != tc.status {
				t.Fatalf("conditional edit status %d, want %d", w.Code, tc.status)
			}
			p, err := h.Store.Pipelines.Get(context.Background(), "p")
			if err != nil {
				t.Fatal("fixture read failed")
			}
			if tc.status == 200 {
				if p.Name != "edited" || p.Version != 2 {
					t.Fatal("accepted edit not persisted")
				}
			} else if p.Name != "original" || p.Version != 1 {
				t.Fatal("refused edit changed stored definition")
			}
		})
	}
}

func TestPipelineConcurrentConditionalEditsHaveOneWinner(t *testing.T) {
	h := newTestHandler(t)
	if h.Store.Pipelines.Create(context.Background(), &model.Pipeline{ID: "p", Name: "original", Version: 1}) != nil {
		t.Fatal("fixture create failed")
	}
	r := gin.New()
	r.PUT("/pipelines/:id", h.UpdatePipeline)
	start := make(chan struct{})
	codes := make(chan int, 2)
	var workers sync.WaitGroup
	for _, name := range []string{"first", "second"} {
		workers.Add(1)
		go func(name string) {
			defer workers.Done()
			<-start
			w := httptest.NewRecorder()
			r.ServeHTTP(w, newRequest(http.MethodPut, "/pipelines/p", &model.Pipeline{Name: name, Version: 1}))
			codes <- w.Code
		}(name)
	}
	close(start)
	workers.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatal("unexpected concurrent edit status")
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("same pipeline generation had multiple edit winners")
	}
}

func TestPipelineCreateReturnsPersistedInitialEditVersion(t *testing.T) {
	h := newTestHandler(t)
	r := gin.New()
	r.POST("/pipelines", h.CreatePipeline)
	r.PUT("/pipelines/:id", h.UpdatePipeline)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodPost, "/pipelines", &model.Pipeline{Name: "created", Version: 999}))
	if w.Code != 201 {
		t.Fatal("create failed")
	}
	var created model.Pipeline
	if json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatal("create response invalid")
	}
	p, err := h.Store.Pipelines.Get(context.Background(), created.ID)
	if err != nil || created.Version != 1 || p.Version != created.Version {
		t.Fatal("create response version disagrees with persisted token")
	}
	created.Name = "edited"
	saved := httptest.NewRecorder()
	r.ServeHTTP(saved, newRequest(http.MethodPut, "/pipelines/"+created.ID, &created))
	if saved.Code != 200 {
		t.Fatal("freshly created version could not be edited")
	}
}
