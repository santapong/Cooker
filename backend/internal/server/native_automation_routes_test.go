package server

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/auth/local"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/handler"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestNativeTriggerWriteRoutesRetainAdminAndMFA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	issuer, err := local.NewIssuer(make([]byte, 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mw, err := auth.NewMiddleware(ctx, config.OIDCConfig{})
	if err != nil {
		t.Fatal(err)
	}
	mw.EnableLocalAuth(issuer)
	codec, err := crypto.NewCodec(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	h := handler.New(st, codec, nil)
	source := &bootSourceFixture{}
	h.Automation = &service.Automation{Triggers: &service.TriggerService{Store: memory.NewTriggerStore()}, Native: &service.PipelineAutomation{Preparer: &service.PipelineSourcePreparer{Store: st, Source: source}}}
	paths := map[string]int{"/trigger-rules/preview": 422, "/trigger-rules": 422, "/trigger-intents/missing/approve": 404, "/trigger-rules/missing/check": 404}
	for _, mfa := range []bool{false, true} {
		cfg := &config.Config{}
		if mfa {
			cfg.OIDC.MFAACRValues = []string{"mfa"}
		}
		r := gin.New()
		s := &Server{router: r, handler: h, config: cfg, oidcMW: mw}
		s.registerRoutes()
		for _, role := range []string{"", "viewer", "operator", "admin"} {
			token := ""
			if role != "" {
				token, _, err = issuer.Issue(role, role+"@fixture.invalid", role, role)
				if err != nil {
					t.Fatal(err)
				}
			}
			for path, adminCode := range paths {
				req := httptest.NewRequest("POST", "/api/v1/admin"+path, strings.NewReader(`{"rule":{"id":"b7edb501-7276-4c96-88bf-7216e3c9d2f1","targetKind":"pipeline","targetId":"missing-native"},"ref":"refs/heads/main"}`))
				req.Header.Set("Content-Type", "application/json")
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				want := 403
				if role == "" {
					want = 401
				} else if role == "admin" && !mfa {
					want = adminCode
				}
				if w.Code != want {
					t.Fatalf("role=%s mfa=%v path=%s status=%d want=%d", role, mfa, path, w.Code, want)
				}
			}
		}
	}
	if source.reads != 0 {
		t.Fatal("denied/unconfigured routes reached source")
	}
	rows, _ := st.Runs.List(ctx, "", 0, 0)
	if len(rows) != 0 {
		t.Fatal("rule administration started a run")
	}
}
