package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// All registry traffic stays inside this owned loopback receiver. No provider
// credentials or public image registry is used for remote admission evidence.
func TestRegistryAuthorityRemoteImageAdmissionUsesExactSubject(t *testing.T) {
	ctx := context.Background()
	st, s, f, r := registryAuthorityFixture(t)
	reg := registry.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		u, p, ok := request.BasicAuth()
		if !ok || u != "ci" || p != registryCanary {
			w.Header().Set("WWW-Authenticate", `Basic realm="owned"`)
			w.WriteHeader(401)
			return
		}
		reg.ServeHTTP(w, request)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	if err := st.Registries.Delete(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	r.URL = server.URL
	if err := st.Registries.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	ref, err := name.ParseReference(host + "/owned/image:v1")
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.Write(ref, empty.Image, remote.WithContext(ctx), remote.WithAuth(authn.FromConfig(authn.AuthConfig{Username: "ci", Password: registryCanary}))); err != nil {
		t.Fatal(err)
	}
	subject := model.RegistrySubject{Kind: "app", ID: "app-a"}
	plan, err := s.Prepare(ctx, subject, []string{r.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := s.Resolver(ctx, subject, plan, "")
	if err != nil {
		t.Fatal(err)
	}
	pin, err := RegistryImageResolver(resolve)(ctx, ref.Name())
	if err != nil || !strings.Contains(pin, "@sha256:") {
		t.Fatal("authorized remote image admission failed", err)
	}
	if f.latestReads != 0 || f.reads != 1 {
		t.Fatal("image admission borrowed latest credential")
	}
	before := f.reads
	emptyPlan, err := s.Prepare(ctx, subject, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	anonymous, err := s.Resolver(ctx, subject, emptyPlan, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = RegistryImageResolver(anonymous)(ctx, ref.Name()); err == nil {
		t.Fatal("empty plan accessed private image")
	}
	if f.reads != before {
		t.Fatal("anonymous remote admission discovered configured credential")
	}
	if _, err = s.Prepare(ctx, model.RegistrySubject{Kind: "app", ID: "app-b"}, []string{r.ID}, nil); err == nil {
		t.Fatal("unrelated App borrowed private image credentials")
	}
}

func TestRegistryAuthorityNoGrantAndResolverSnapshotCannotExpand(t *testing.T) {
	ctx := context.Background()
	st, s, f, r := registryAuthorityFixture(t)
	subject := model.RegistrySubject{Kind: "app", ID: "app-a"}
	plan, err := s.Prepare(ctx, subject, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolve, err := s.Resolver(ctx, subject, plan, "")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := s.Prepare(ctx, subject, []string{r.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan.Connections = selected.Connections
	a, err := resolve(ctx, "ghcr.io")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Authorization()
	if cfg.Password != "" || f.reads != 0 {
		t.Fatal("caller expanded previously empty resolver")
	}
	if _, err = st.Registries.(store.RegistryGrantStore).UpdateGrants(ctx, r.ID, r.Version, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := f.pins
	if _, err = s.Prepare(ctx, subject, []string{r.ID}, nil); err == nil || f.pins != before {
		t.Fatal("default deny read configured credentials")
	}
}
