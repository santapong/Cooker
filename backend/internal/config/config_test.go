package config

import (
	"encoding/base64"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear any env vars that might interfere
	os.Unsetenv("COOKER_ENV")
	os.Unsetenv("COOKER_PORT")
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("REDIS_URL")
	os.Unsetenv("COOKER_OIDC_ENABLED")
	os.Unsetenv("COOKER_ALLOWED_ORIGINS")

	cfg := Load()

	if cfg.Env != EnvDev {
		t.Errorf("expected default Env=dev, got %q", cfg.Env)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://cooker:cooker@localhost:5432/cooker?sslmode=disable" {
		t.Errorf("unexpected default database URL: %s", cfg.DatabaseURL)
	}
	if cfg.RedisURL != "redis://localhost:6379" {
		t.Errorf("unexpected default redis URL: %s", cfg.RedisURL)
	}
	if cfg.OIDC.Enabled {
		t.Error("OIDC should be disabled by default")
	}
	if cfg.Docker.Host != "unix:///var/run/docker.sock" {
		t.Errorf("unexpected default docker host: %s", cfg.Docker.Host)
	}
	if cfg.Kubernetes.InCluster {
		t.Error("Kubernetes in-cluster should be false by default")
	}
	wantOrigins := []string{"http://localhost:5173", "http://localhost:3000"}
	if !reflect.DeepEqual(cfg.AllowedOrigins, wantOrigins) {
		t.Errorf("unexpected default AllowedOrigins: %v", cfg.AllowedOrigins)
	}
}

func TestLoad_CustomPort(t *testing.T) {
	os.Setenv("COOKER_PORT", "9090")
	defer os.Unsetenv("COOKER_PORT")

	cfg := Load()
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	os.Setenv("COOKER_PORT", "not-a-number")
	defer os.Unsetenv("COOKER_PORT")

	cfg := Load()
	if cfg.Port != 8080 {
		t.Errorf("expected fallback port 8080, got %d", cfg.Port)
	}
}

func TestLoad_OIDCEnabled(t *testing.T) {
	os.Setenv("COOKER_OIDC_ENABLED", "true")
	os.Setenv("COOKER_OIDC_ISSUER_URL", "https://auth.example.com")
	os.Setenv("COOKER_OIDC_CLIENT_ID", "my-client")
	defer func() {
		os.Unsetenv("COOKER_OIDC_ENABLED")
		os.Unsetenv("COOKER_OIDC_ISSUER_URL")
		os.Unsetenv("COOKER_OIDC_CLIENT_ID")
	}()

	cfg := Load()
	if !cfg.OIDC.Enabled {
		t.Error("expected OIDC to be enabled")
	}
	if cfg.OIDC.IssuerURL != "https://auth.example.com" {
		t.Errorf("unexpected issuer URL: %s", cfg.OIDC.IssuerURL)
	}
	if cfg.OIDC.ClientID != "my-client" {
		t.Errorf("unexpected client ID: %s", cfg.OIDC.ClientID)
	}
}

func TestLoad_DockerConfig(t *testing.T) {
	os.Setenv("DOCKER_HOST", "tcp://remote:2376")
	os.Setenv("DOCKER_TLS_VERIFY", "true")
	os.Setenv("DOCKER_CERT_PATH", "/certs")
	defer func() {
		os.Unsetenv("DOCKER_HOST")
		os.Unsetenv("DOCKER_TLS_VERIFY")
		os.Unsetenv("DOCKER_CERT_PATH")
	}()

	cfg := Load()
	if cfg.Docker.Host != "tcp://remote:2376" {
		t.Errorf("unexpected docker host: %s", cfg.Docker.Host)
	}
	if !cfg.Docker.TLSVerify {
		t.Error("expected TLSVerify to be true")
	}
	if cfg.Docker.CertPath != "/certs" {
		t.Errorf("unexpected cert path: %s", cfg.Docker.CertPath)
	}
}

func TestLoad_KubernetesConfig(t *testing.T) {
	os.Setenv("COOKER_K8S_IN_CLUSTER", "true")
	os.Setenv("KUBECONFIG", "/home/user/.kube/config")
	defer func() {
		os.Unsetenv("COOKER_K8S_IN_CLUSTER")
		os.Unsetenv("KUBECONFIG")
	}()

	cfg := Load()
	if !cfg.Kubernetes.InCluster {
		t.Error("expected InCluster to be true")
	}
	if cfg.Kubernetes.Kubeconfig != "/home/user/.kube/config" {
		t.Errorf("unexpected kubeconfig: %s", cfg.Kubernetes.Kubeconfig)
	}
}

func TestLoad_OIDCScopes(t *testing.T) {
	cfg := Load()
	expectedScopes := []string{"openid", "profile", "email", "groups"}
	if len(cfg.OIDC.Scopes) != len(expectedScopes) {
		t.Fatalf("expected %d scopes, got %d", len(expectedScopes), len(cfg.OIDC.Scopes))
	}
	for i, s := range expectedScopes {
		if cfg.OIDC.Scopes[i] != s {
			t.Errorf("expected scope %s at index %d, got %s", s, i, cfg.OIDC.Scopes[i])
		}
	}
}

func TestLoad_InvalidBool(t *testing.T) {
	os.Setenv("COOKER_OIDC_ENABLED", "not-a-bool")
	defer os.Unsetenv("COOKER_OIDC_ENABLED")

	cfg := Load()
	if cfg.OIDC.Enabled {
		t.Error("expected fallback false for invalid bool")
	}
}

func TestLoad_AllowedOriginsCustom(t *testing.T) {
	os.Setenv("COOKER_ALLOWED_ORIGINS", "https://app.example.com, https://admin.example.com")
	defer os.Unsetenv("COOKER_ALLOWED_ORIGINS")

	cfg := Load()
	want := []string{"https://app.example.com", "https://admin.example.com"}
	if !reflect.DeepEqual(cfg.AllowedOrigins, want) {
		t.Errorf("AllowedOrigins = %v, want %v", cfg.AllowedOrigins, want)
	}
}

func TestLoad_AllowedOriginsWildcard(t *testing.T) {
	os.Setenv("COOKER_ALLOWED_ORIGINS", "*")
	defer os.Unsetenv("COOKER_ALLOWED_ORIGINS")

	cfg := Load()
	if !reflect.DeepEqual(cfg.AllowedOrigins, []string{"*"}) {
		t.Errorf("AllowedOrigins = %v, want [\"*\"]", cfg.AllowedOrigins)
	}
}

func TestLoad_AllowedOriginsDefaultsByEnv(t *testing.T) {
	os.Unsetenv("COOKER_ALLOWED_ORIGINS")

	t.Run("dev defaults to localhost", func(t *testing.T) {
		os.Setenv("COOKER_ENV", "dev")
		defer os.Unsetenv("COOKER_ENV")
		cfg := Load()
		want := []string{"http://localhost:5173", "http://localhost:3000"}
		if !reflect.DeepEqual(cfg.AllowedOrigins, want) {
			t.Errorf("dev AllowedOrigins = %v, want %v", cfg.AllowedOrigins, want)
		}
	})

	t.Run("uat defaults to localhost", func(t *testing.T) {
		os.Setenv("COOKER_ENV", "uat")
		defer os.Unsetenv("COOKER_ENV")
		cfg := Load()
		if len(cfg.AllowedOrigins) == 0 {
			t.Error("uat should have non-empty AllowedOrigins default")
		}
	})

	t.Run("production has empty default", func(t *testing.T) {
		os.Setenv("COOKER_ENV", "production")
		defer os.Unsetenv("COOKER_ENV")
		cfg := Load()
		if len(cfg.AllowedOrigins) != 0 {
			t.Errorf("production AllowedOrigins should be empty by default, got %v", cfg.AllowedOrigins)
		}
	})

	t.Run("production with explicit origins keeps them", func(t *testing.T) {
		os.Setenv("COOKER_ENV", "production")
		os.Setenv("COOKER_ALLOWED_ORIGINS", "https://cooker.example.com")
		defer os.Unsetenv("COOKER_ENV")
		defer os.Unsetenv("COOKER_ALLOWED_ORIGINS")
		cfg := Load()
		want := []string{"https://cooker.example.com"}
		if !reflect.DeepEqual(cfg.AllowedOrigins, want) {
			t.Errorf("production AllowedOrigins = %v, want %v", cfg.AllowedOrigins, want)
		}
	})
}

func TestEnv_IsProduction(t *testing.T) {
	cases := []struct {
		env  Env
		want bool
	}{
		{EnvDev, false},
		{EnvUAT, false},
		{EnvProduction, true},
		{Env(""), false},
		{Env("staging"), false},
	}
	for _, c := range cases {
		if got := c.env.IsProduction(); got != c.want {
			t.Errorf("Env(%q).IsProduction() = %v, want %v", c.env, got, c.want)
		}
	}
}

// 32 random bytes base64-encoded — pre-computed so tests don't need crypto/rand.
const validSecretKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

func TestValidate_DevPermissive(t *testing.T) {
	cfg := &Config{Env: EnvDev}
	if err := cfg.Validate(); err != nil {
		t.Errorf("dev should be permissive, got error: %v", err)
	}
}

func TestValidate_UATPermissive(t *testing.T) {
	cfg := &Config{Env: EnvUAT}
	if err := cfg.Validate(); err != nil {
		t.Errorf("uat should be permissive, got error: %v", err)
	}
}

func TestValidate_ProductionRequiresSecretKey(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		AllowedOrigins: []string{"https://cooker.example.com"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when SecretKey missing in production")
	}
	if !strings.Contains(err.Error(), "COOKER_SECRET_KEY") {
		t.Errorf("error should mention COOKER_SECRET_KEY, got: %v", err)
	}
}

func TestValidate_ProductionShortSecretKey(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		SecretKey:      base64.StdEncoding.EncodeToString([]byte("short")),
		AllowedOrigins: []string{"https://cooker.example.com"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for too-short SecretKey")
	}
	if !strings.Contains(err.Error(), "32") {
		t.Errorf("error should mention 32-byte requirement, got: %v", err)
	}
}

func TestValidate_ProductionInvalidBase64SecretKey(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		SecretKey:      "not!!base64!!at!!all",
		AllowedOrigins: []string{"https://cooker.example.com"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for non-base64 SecretKey")
	}
	if !strings.Contains(err.Error(), "base64") {
		t.Errorf("error should mention base64, got: %v", err)
	}
}

func TestValidate_ProductionRequiresAllowedOrigins(t *testing.T) {
	cfg := &Config{
		Env:       EnvProduction,
		SecretKey: validSecretKey,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when AllowedOrigins empty in production")
	}
	if !strings.Contains(err.Error(), "COOKER_ALLOWED_ORIGINS") {
		t.Errorf("error should mention COOKER_ALLOWED_ORIGINS, got: %v", err)
	}
}

func TestValidate_ProductionHappyPath(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://prod:prod@db.example.com:5432/cooker?sslmode=require",
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		OIDC:           OIDCConfig{Enabled: true},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("production happy path should validate, got: %v", err)
	}
}

func TestValidate_ProductionRejectsDefaultDatabaseURL(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://cooker:cooker@localhost:5432/cooker?sslmode=disable",
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		OIDC:           OIDCConfig{Enabled: true},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected production to reject the dev-default DATABASE_URL")
	} else if !strings.Contains(err.Error(), "dev default") {
		t.Fatalf("error should mention dev default, got: %v", err)
	}
}

func TestValidate_ProductionRejectsWildcardOrigin(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://prod:prod@db.example.com:5432/cooker?sslmode=require",
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"*"},
		OIDC:           OIDCConfig{Enabled: true},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "*") {
		t.Fatalf("production should reject AllowedOrigins=[\"*\"], got: %v", err)
	}
}

func TestValidate_ProductionRejectsSSLModeDisable(t *testing.T) {
	// S26-05-10: real DATABASE_URL with sslmode=disable must fail.
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://prod:realpw@db.cooker.svc.cluster.local:5432/cooker?sslmode=disable",
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		OIDC:           OIDCConfig{Enabled: true},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("expected sslmode rejection, got: %v", err)
	}
}

func TestValidate_ProductionRejectsMissingSSLMode(t *testing.T) {
	// S26-05-10: a non-localhost DATABASE_URL with no sslmode query
	// parameter at all is equivalent to disable for the purposes of
	// this guard.
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://prod:realpw@db.cooker.svc.cluster.local:5432/cooker",
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		OIDC:           OIDCConfig{Enabled: true},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("expected sslmode rejection for missing query param, got: %v", err)
	}
}

func TestValidate_ProductionAcceptsVerifyFull(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://prod:realpw@db.example.com:5432/cooker?sslmode=verify-full",
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		OIDC:           OIDCConfig{Enabled: true},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("sslmode=verify-full should be accepted, got: %v", err)
	}
}

func TestValidate_ProductionAllowsLocalhostWithoutSSL(t *testing.T) {
	// The sslmode guard only applies to non-localhost hosts: a
	// loopback DATABASE_URL is dev-only-shaped and already covered by
	// the dev-default-rejection check. We don't want to false-positive
	// here for operators who happen to run an in-pod Postgres on
	// localhost.
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://prod:realpw@localhost:5432/cooker?sslmode=disable",
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		OIDC:           OIDCConfig{Enabled: true},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("localhost DATABASE_URL should not trigger sslmode guard, got: %v", err)
	}
}

func TestValidate_ProductionRejectsHTTPKeepSave(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://prod:prod@db.example.com:5432/cooker?sslmode=require",
		SecretsBackend: "keepsave",
		KeepSave:       KeepSaveConfig{URL: "http://keepsave:8080", ProjectID: "p", APIKey: "k"},
		AllowedOrigins: []string{"https://cooker.example.com"},
		OIDC:           OIDCConfig{Enabled: true},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "https://") {
		t.Fatalf("production should reject http:// KeepSave URL, got: %v", err)
	}
}

func TestParseGroupRoleMap(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"single pair", "platform-admins:admin", map[string]string{"platform-admins": "admin"}},
		{"multi pair", "g1:admin,g2:operator", map[string]string{"g1": "admin", "g2": "operator"}},
		{"trims spaces", "  g1 : admin , g2 : operator ", map[string]string{"g1": "admin", "g2": "operator"}},
		{"skips malformed", "g1:admin,oops,g2:operator", map[string]string{"g1": "admin", "g2": "operator"}},
		{"skips empty halves", "g1:admin,:operator,g2:", map[string]string{"g1": "admin"}},
		{"all malformed yields nil", "oops,nope", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGroupRoleMap(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseGroupRoleMap(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestLoad_OIDCGroupMap(t *testing.T) {
	os.Setenv("COOKER_OIDC_GROUP_MAP", "platform-admins:admin,platform-eng:operator")
	defer os.Unsetenv("COOKER_OIDC_GROUP_MAP")

	cfg := Load()
	want := map[string]string{
		"platform-admins": "admin",
		"platform-eng":    "operator",
	}
	if !reflect.DeepEqual(cfg.OIDC.GroupRoleMap, want) {
		t.Errorf("GroupRoleMap = %v, want %v", cfg.OIDC.GroupRoleMap, want)
	}
}

func TestValidate_ProductionAccumulatesProblems(t *testing.T) {
	cfg := &Config{Env: EnvProduction}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "COOKER_SECRET_KEY") || !strings.Contains(err.Error(), "COOKER_ALLOWED_ORIGINS") {
		t.Errorf("expected both problems in error, got: %v", err)
	}
}

func TestValidate_ProductionRefusesDockerBuilder(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		BuilderBackend: "docker",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "COOKER_BUILDER=docker") {
		t.Fatalf("expected docker-builder rejection, got: %v", err)
	}
}

func TestValidate_ProductionMultiReplicaRequiresRedisOrSticky(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		ReplicaCount:   3,
		RateLimit:      RateLimitConfig{Enabled: true, Backend: "memory"},
		WSTicket:       WSTicketConfig{Backend: "memory"},
		WSHub:          WSHubConfig{Backend: "memory"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected multi-replica + memory backend rejection")
	}
	for _, want := range []string{"COOKER_RATE_LIMIT_BACKEND", "COOKER_WS_TICKET_BACKEND", "COOKER_WS_HUB_BACKEND", "COOKER_STICKY_SESSIONS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error to mention %s, got: %v", want, err)
		}
	}
}

func TestValidate_ProductionMultiReplicaWithStickyOK(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://prod:prod@db.example.com:5432/cooker?sslmode=require",
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		ReplicaCount:   3,
		StickySessions: true,
		RateLimit:      RateLimitConfig{Enabled: true, Backend: "memory"},
		WSTicket:       WSTicketConfig{Backend: "memory"},
		WSHub:          WSHubConfig{Backend: "memory"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("sticky sessions should satisfy multi-replica guard, got: %v", err)
	}
}

func TestValidate_ProductionMultiReplicaWithRedisOK(t *testing.T) {
	cfg := &Config{
		Env:            EnvProduction,
		DatabaseURL:    "postgres://prod:prod@db.example.com:5432/cooker?sslmode=require",
		SecretKey:      validSecretKey,
		AllowedOrigins: []string{"https://cooker.example.com"},
		ReplicaCount:   3,
		RateLimit:      RateLimitConfig{Enabled: true, Backend: "redis"},
		WSTicket:       WSTicketConfig{Backend: "redis"},
		WSHub:          WSHubConfig{Backend: "redis"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("redis backends should satisfy multi-replica guard, got: %v", err)
	}
}
