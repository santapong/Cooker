package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

type deniedRegistrySecretFixture struct{ calls int }

func (f *deniedRegistrySecretFixture) Get(context.Context, string, string) ([]byte, error) {
	f.calls++
	return []byte("owned-registry-secret-canary"), nil
}
func (f *deniedRegistrySecretFixture) List(context.Context, string) ([]string, error) {
	f.calls++
	return []string{"password"}, nil
}
func (f *deniedRegistrySecretFixture) Put(context.Context, string, string, []byte) error { f.calls++; return nil }
func (f *deniedRegistrySecretFixture) Delete(context.Context, string, string) error { f.calls++; return nil }
func (f *deniedRegistrySecretFixture) Promote(context.Context, string, string, []string) error { f.calls++; return nil }

func TestRegistryEnvironmentGeneralControlsDoNotAccessCredentials(t *testing.T) {
	h := newTestHandler(t)
	f := &deniedRegistrySecretFixture{}
	h.Secrets = f
	ctx := context.Background()
	for _, env := range []*model.Environment{
		{ID: "_registries", Name: "_registries (internal)"},
		{ID: "registry_owned", Name: "registry_owned"},
		{ID: "friendly-alias", Name: " REGISTRY_owned "},
		{ID: "normal", Name: "Production"},
	} {
		if err := h.Store.Environments.Create(ctx, env); err != nil { t.Fatal(err) }
	}
	admin := &auth.Claims{Subject: "owned-admin", Roles: []string{string(auth.RoleAdmin)}}
	r := gin.New()
	r.GET("/environments/:id/secrets/:key", withUser(h.RevealSecret, admin))
	r.PUT("/environments/:id/secrets/:key", withUser(h.PutSecret, admin))
	r.DELETE("/environments/:id/secrets/:key", withUser(h.DeleteSecret, admin))
	r.POST("/environments/:id/secrets/promote", withUser(h.PromoteSecrets, admin))
	r.PUT("/environments/:id", withUser(h.UpdateEnvironment, admin))
	r.DELETE("/environments/:id", withUser(h.DeleteEnvironment, admin))
	for _, id := range []string{"_registries", "registry_owned", "friendly-alias"} {
		for _, tc := range []struct{ method, path string; body any }{
			{http.MethodGet, "/secrets/password", nil},
			{http.MethodPut, "/secrets/password", map[string]string{"value":"replacement"}},
			{http.MethodDelete, "/secrets/password", nil},
			{http.MethodPost, "/secrets/promote", map[string]string{"toEnvironmentId":"normal"}},
			{http.MethodPut, "", model.Environment{Name:"renamed"}},
			{http.MethodDelete, "", nil},
		} {
			t.Run(id+tc.method+tc.path, func(t *testing.T) {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, newRequest(tc.method, "/environments/"+id+tc.path, tc.body))
				if w.Code != http.StatusNotFound { t.Fatalf("private scope visible: HTTP%d", w.Code) }
				if f.calls != 0 || strings.Contains(w.Body.String(), "canary") { t.Fatal("private credential accessed") }
			})
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, newRequest(http.MethodPost, "/environments/normal/secrets/promote", map[string]string{"toEnvironmentId":id}))
		if w.Code != http.StatusNotFound || f.calls != 0 { t.Fatal("private promotion target accessed") }
		if _, err := h.Store.Environments.Get(ctx, id); err != nil { t.Fatal("private environment mutated") }
	}
}

func TestRegistryEnvironmentPublicPaginationAndReservedNames(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	for _, env := range []*model.Environment{
		{ID:"registry_first", Name:"private", Order:0},
		{ID:"alias", Name:" _REGISTRIES (internal) ", Order:1},
		{ID:"public-a", Name:"Development", Order:2},
		{ID:"public-b", Name:"Production", Order:3},
		{ID:"public-c", Name:"Preview", Order:4},
	} { if err := h.Store.Environments.Create(ctx, env); err != nil { t.Fatal(err) } }
	viewer := &auth.Claims{Roles:[]string{string(auth.RoleViewer)}}
	r := gin.New()
	r.GET("/environments", withUser(h.ListEnvironments, viewer))
	r.POST("/environments", h.CreateEnvironment)
	r.PUT("/environments/:id", h.UpdateEnvironment)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRequest(http.MethodGet, "/environments?limit=1&offset=1", nil))
	var rows []model.Environment
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &rows) != nil || len(rows)!=1 || rows[0].ID!="public-b" {
		t.Fatal("pagination applied before private filtering")
	}
	internal, err := h.Store.Environments.List(ctx,0,0)
	if err != nil || len(internal)!=5 { t.Fatal("internal credential manager inventory changed") }
	if _, ok := h.Store.Environments.(store.PublicEnvironmentLister); !ok { t.Fatal("public listing contract missing") }
	for _, name := range []string{"registry_private", " REGISTRY_private ", "_registries", " _REGISTRIES (internal) "} {
		for _, method := range []string{http.MethodPost,http.MethodPut} {
			path := "/environments"
			if method==http.MethodPut {path+="/public-a"}
			w=httptest.NewRecorder(); r.ServeHTTP(w,newRequest(method,path,model.Environment{Name:name}))
			if w.Code!=http.StatusBadRequest { t.Fatalf("reserved alias accepted: HTTP%d",w.Code) }
		}
	}
}

func TestRegistryEnvironmentAppAndPipelineBindingsRejectedBeforeSourceAccess(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	if err:=h.Store.Environments.Create(ctx,&model.Environment{ID:"friendly-alias",Name:"registry_owned"});err!=nil{t.Fatal(err)}
	r:=gin.New(); r.POST("/apps",h.CreateApp);r.POST("/pipelines",h.CreatePipeline)
	for _, id:=range []string{"_registries","registry_owned","friendly-alias"} {
		for _,tc:=range []struct{path string;body any}{
			{"/apps",model.App{Name:"owned-app",GitHubRepo:"acme/app",EnvironmentID:id}},
			{"/pipelines",model.Pipeline{Name:"owned-pipeline",Stages:[]model.Stage{{ID:"build",Type:model.StageTypeBuild,EnvironmentID:id}}}},
		} {
			w:=httptest.NewRecorder();r.ServeHTTP(w,newRequest(http.MethodPost,tc.path,tc.body))
			if w.Code!=http.StatusBadRequest || !strings.Contains(w.Body.String(),"credential-only") {t.Fatalf("private binding accepted: HTTP%d %s",w.Code,w.Body.String())}
		}
	}
	apps,_:=h.Store.Apps.List(ctx,0,0);pipelines,_:=h.Store.Pipelines.List(ctx,0,0)
	if len(apps)!=0 || len(pipelines)!=0 {t.Fatal("rejected binding persisted")}
}
