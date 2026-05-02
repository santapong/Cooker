package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// Env represents the deployment environment. Affects defaults and
// strictness of startup validation. Values: "dev", "uat", "production".
type Env string

const (
	EnvDev        Env = "dev"
	EnvUAT        Env = "uat"
	EnvProduction Env = "production"
)

// IsProduction reports whether the environment is production.
func (e Env) IsProduction() bool { return e == EnvProduction }

// Config holds all application configuration.
type Config struct {
	// Env identifies the deployment environment. Production gates
	// strict defaults: empty AllowedOrigins becomes deny-all,
	// missing SecretKey becomes fatal at boot, etc.
	Env            Env
	Port           int
	DatabaseURL    string
	RedisURL       string
	AllowedOrigins []string
	// SecretKey is a base64-encoded 32-byte key for AES-GCM
	// encryption of secrets at rest. Empty disables the secret API.
	SecretKey string
	// Registry is the default image registry prefix used when an
	// App doesn't override it (e.g., "registry.example.com/cooker").
	Registry string
	// Backends select runtime implementations. Empty defaults to
	// "noop" so tests and dev boot; UAT sets these to real values.
	BuilderBackend  string // "noop" | "docker" | "buildkit"
	PusherBackend   string // "noop" | "docker" | "crane"
	DeployerBackend string // "noop" | "kubectl" | "clientgo"
	// RateLimit tunes the per-user limiter on expensive endpoints.
	// PerMinute<=0 disables the middleware (passthrough). Multi-replica
	// deployments should disable this and rely on edge rate limiting.
	RateLimit       RateLimitConfig
	OIDC            OIDCConfig
	Docker          DockerConfig
	Kubernetes      KubernetesConfig
}

// RateLimitConfig tunes per-user rate limiting on expensive endpoints
// (pipeline runs, image builds, app deploys). It is in-memory and
// per-process; multi-replica deployments should set Enabled=false
// and use edge-level (ingress / WAF) rate limiting instead.
type RateLimitConfig struct {
	Enabled   bool
	PerMinute int
	Burst     int
}

// OIDCConfig holds SSO/OIDC authentication configuration.
type OIDCConfig struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

// DockerConfig holds Docker Engine connection settings.
type DockerConfig struct {
	Host      string // e.g., "unix:///var/run/docker.sock" or "tcp://host:2376"
	TLSVerify bool
	CertPath  string
}

// KubernetesConfig holds Kubernetes connection settings.
type KubernetesConfig struct {
	InCluster  bool
	Kubeconfig string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	env := Env(getEnv("COOKER_ENV", string(EnvDev)))
	// In production, AllowedOrigins must be configured explicitly —
	// no permissive localhost defaults. PR B adds startup validation
	// that turns this into a fatal error if still empty at boot.
	originDefault := []string{"http://localhost:5173", "http://localhost:3000"}
	if env.IsProduction() {
		originDefault = nil
	}
	return &Config{
		Env:            env,
		Port:           getEnvInt("COOKER_PORT", 8080),
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://cooker:cooker@localhost:5432/cooker?sslmode=disable"),
		RedisURL:       getEnv("REDIS_URL", "redis://localhost:6379"),
		AllowedOrigins: getEnvCSV("COOKER_ALLOWED_ORIGINS", originDefault),
		SecretKey:       getEnv("COOKER_SECRET_KEY", ""),
		Registry:        getEnv("COOKER_REGISTRY", "localhost:5000/cooker"),
		BuilderBackend:  getEnv("COOKER_BUILDER", "noop"),
		PusherBackend:   getEnv("COOKER_PUSHER", "noop"),
		DeployerBackend: getEnv("COOKER_DEPLOYER", "noop"),
		RateLimit: RateLimitConfig{
			Enabled:   getEnvBool("COOKER_RATE_LIMIT_ENABLED", true),
			PerMinute: getEnvInt("COOKER_RATE_LIMIT_PER_MINUTE", 10),
			Burst:     getEnvInt("COOKER_RATE_LIMIT_BURST", 3),
		},
		OIDC: OIDCConfig{
			Enabled:      getEnvBool("COOKER_OIDC_ENABLED", false),
			IssuerURL:    getEnv("COOKER_OIDC_ISSUER_URL", ""),
			ClientID:     getEnv("COOKER_OIDC_CLIENT_ID", ""),
			ClientSecret: getEnv("COOKER_OIDC_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("COOKER_OIDC_REDIRECT_URL", ""),
			Scopes:       []string{"openid", "profile", "email", "groups"},
		},
		Docker: DockerConfig{
			Host:      getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
			TLSVerify: getEnvBool("DOCKER_TLS_VERIFY", false),
			CertPath:  getEnv("DOCKER_CERT_PATH", ""),
		},
		Kubernetes: KubernetesConfig{
			InCluster:  getEnvBool("COOKER_K8S_IN_CLUSTER", false),
			Kubeconfig: getEnv("KUBECONFIG", ""),
		},
	}
}

// Validate enforces production-mode invariants. It is called from
// main after Load. Errors returned are intended to be fatal.
//
// Rules (production only):
//   - SecretKey must be present and decode to >= 32 bytes (AES-256).
//   - AllowedOrigins must be set explicitly (the default is empty
//     in production, so a missing operator config is loud).
//   - OIDC enabled is recommended but not enforced — running with
//     OIDC disabled in production logs a warning so operators see
//     the gap in their logs without blocking deployment.
//
// dev and uat skip these checks so contributors and testers can
// boot without setting any of them.
func (c *Config) Validate() error {
	if !c.Env.IsProduction() {
		return nil
	}
	var problems []string
	if c.SecretKey == "" {
		problems = append(problems, "COOKER_SECRET_KEY is required in production (used to encrypt secrets at rest)")
	} else {
		// SecretKey is base64. Decode and check length.
		decoded, err := base64.StdEncoding.DecodeString(c.SecretKey)
		switch {
		case err != nil:
			problems = append(problems, "COOKER_SECRET_KEY must be a base64-encoded 32-byte key")
		case len(decoded) < 32:
			problems = append(problems, fmt.Sprintf("COOKER_SECRET_KEY decodes to %d bytes; need at least 32 (AES-256)", len(decoded)))
		}
	}
	if len(c.AllowedOrigins) == 0 {
		problems = append(problems, "COOKER_ALLOWED_ORIGINS is required in production (no permissive default)")
	}
	if !c.OIDC.Enabled {
		log.Printf("warning: COOKER_OIDC_ENABLED=false in production; the backend will inject a dev admin user on every request")
	}
	if len(problems) > 0 {
		return errors.New("config: " + strings.Join(problems, "; "))
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvCSV(key string, fallback []string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
