package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration.
type Config struct {
	Port           int
	DatabaseURL    string
	RedisURL       string
	AllowedOrigins []string
	// SecretKey is a base64-encoded 32-byte key for AES-GCM
	// encryption of secrets at rest. Empty disables the secret API.
	SecretKey  string
	OIDC       OIDCConfig
	Docker     DockerConfig
	Kubernetes KubernetesConfig
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
	return &Config{
		Port:           getEnvInt("COOKER_PORT", 8080),
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://cooker:cooker@localhost:5432/cooker?sslmode=disable"),
		RedisURL:       getEnv("REDIS_URL", "redis://localhost:6379"),
		AllowedOrigins: getEnvCSV("COOKER_ALLOWED_ORIGINS", []string{"http://localhost:5173", "http://localhost:3000"}),
		SecretKey:      getEnv("COOKER_SECRET_KEY", ""),
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
