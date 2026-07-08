package config

import (
	"strings"
	"time"
)

func Load() *Config {
	env := Env(getEnv("COOKER_ENV", string(EnvDev)))
	originDefault := []string{"http://localhost:5173", "http://localhost:3000"}
	if env.IsProduction() {
		originDefault = nil
	}
	return &Config{
		Env:             env,
		Port:            getEnvInt("COOKER_PORT", 8080),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://cooker:cooker@localhost:5432/cooker?sslmode=disable"),
		RedisURL:        getEnv("REDIS_URL", "redis://localhost:6379"),
		AllowedOrigins:  getEnvCSV("COOKER_ALLOWED_ORIGINS", originDefault),
		SecretKey:       getEnv("COOKER_SECRET_KEY", ""),
		Registry:        getEnv("COOKER_REGISTRY", "localhost:5000/cooker"),
		BuildCacheRepo:  getEnv("COOKER_BUILD_CACHE_REPO", ""),
		BuilderBackend:  getEnv("COOKER_BUILDER", "noop"),
		StageRunner:     getEnv("COOKER_STAGE_RUNNER", "noop"),
		PusherBackend:   getEnv("COOKER_PUSHER", "noop"),
		DeployerBackend: getEnv("COOKER_DEPLOYER", "noop"),
		SecretsBackend:  getEnv("COOKER_SECRETS_BACKEND", "database"),
		ReplicaCount:    getEnvInt("COOKER_REPLICA_COUNT", 1),
		StickySessions:  getEnvBool("COOKER_STICKY_SESSIONS", false),
		RateLimit: RateLimitConfig{
			Enabled:   getEnvBool("COOKER_RATE_LIMIT_ENABLED", true),
			PerMinute: getEnvInt("COOKER_RATE_LIMIT_PER_MINUTE", 10),
			Burst:     getEnvInt("COOKER_RATE_LIMIT_BURST", 3),
			Backend:   getEnv("COOKER_RATE_LIMIT_BACKEND", "memory"),
			Webhook: WebhookRateLimitConfig{
				Enabled:   getEnvBool("COOKER_WEBHOOK_RATE_LIMIT_ENABLED", true),
				PerMinute: getEnvInt("COOKER_WEBHOOK_RATE_LIMIT_PER_MINUTE", 60),
				Burst:     getEnvInt("COOKER_WEBHOOK_RATE_LIMIT_BURST", 10),
			},
		},
		WSTicket: WSTicketConfig{Backend: getEnv("COOKER_WS_TICKET_BACKEND", "memory")},
		WSHub:    WSHubConfig{Backend: getEnv("COOKER_WS_HUB_BACKEND", "memory")},
		OIDC: OIDCConfig{
			Enabled:      getEnvBool("COOKER_OIDC_ENABLED", false),
			IssuerURL:    getEnv("COOKER_OIDC_ISSUER_URL", ""),
			ClientID:     getEnv("COOKER_OIDC_CLIENT_ID", ""),
			ClientSecret: getEnv("COOKER_OIDC_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("COOKER_OIDC_REDIRECT_URL", ""),
			Scopes:       []string{"openid", "profile", "email", "groups"},
			GroupRoleMap: parseGroupRoleMap(getEnv("COOKER_OIDC_GROUP_MAP", "")),
			MFAACRValues: getEnvCSV("COOKER_OIDC_MFA_ACR_VALUES", nil),
		},
		LocalAuth: LocalAuthConfig{
			Enabled:       getEnvBool("COOKER_LOCAL_AUTH_ENABLED", false),
			JWTSigningKey: getEnv("COOKER_LOCAL_AUTH_JWT_SIGNING_KEY", ""),
			TokenTTL:      getEnvDuration("COOKER_LOCAL_AUTH_TOKEN_TTL", 12*time.Hour),
			AllowSignup:   getEnvBool("COOKER_LOCAL_AUTH_ALLOW_SIGNUP", true),
		},
		Docker: DockerConfig{
			Host:      getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
			TLSVerify: getEnvBool("DOCKER_TLS_VERIFY", false),
			CertPath:  getEnv("DOCKER_CERT_PATH", ""),
		},
		Kubernetes: KubernetesConfig{
			InCluster:             getEnvBool("COOKER_K8S_IN_CLUSTER", false),
			Kubeconfig:            getEnv("KUBECONFIG", ""),
			Namespace:             getEnv("COOKER_K8S_NAMESPACE", "cooker"),
			KanikoImage:           getEnv("COOKER_KANIKO_IMAGE", "gcr.io/kaniko-project/executor:latest"),
			KanikoServiceAccount:  getEnv("COOKER_KANIKO_SERVICE_ACCOUNT", ""),
			KanikoContextPVC:      getEnv("COOKER_KANIKO_CONTEXT_PVC", ""),
			BuildahImage:          getEnv("COOKER_BUILDAH_IMAGE", "quay.io/buildah/stable:latest"),
			BuildahServiceAccount: getEnv("COOKER_BUILDAH_SERVICE_ACCOUNT", ""),
			BuildahContextPVC:     getEnv("COOKER_BUILDAH_CONTEXT_PVC", ""),
			BuildahStorageDriver:  getEnv("COOKER_BUILDAH_STORAGE_DRIVER", "vfs"),
		},
		KeepSave: KeepSaveConfig{
			URL:       getEnv("COOKER_SECRETS_KEEPSAVE_URL", ""),
			ProjectID: getEnv("COOKER_SECRETS_KEEPSAVE_PROJECT_ID", ""),
			APIKey:    getEnv("COOKER_SECRETS_KEEPSAVE_API_KEY", ""),
		},
		Vault: VaultConfig{
			Addr:   getEnv("COOKER_SECRETS_VAULT_ADDR", ""),
			Token:  getEnv("COOKER_SECRETS_VAULT_TOKEN", ""),
			Mount:  getEnv("COOKER_SECRETS_VAULT_MOUNT", "secret"),
			Prefix: getEnv("COOKER_SECRETS_VAULT_PREFIX", "cooker"),
		},
		AWSSecrets: AWSSecretsConfig{
			Region: getEnv("COOKER_SECRETS_AWS_REGION", ""),
			Prefix: getEnv("COOKER_SECRETS_AWS_PREFIX", "cooker"),
		},
		GCPSecrets: GCPSecretsConfig{
			ProjectID: getEnv("COOKER_SECRETS_GCP_PROJECT_ID", ""),
			Prefix:    getEnv("COOKER_SECRETS_GCP_PREFIX", "cooker"),
		},
		DeployTargets: DeployTargetsConfig{
			CloudRunProject:   getEnv("COOKER_DEPLOY_CLOUDRUN_PROJECT", ""),
			CloudRunRegion:    getEnv("COOKER_DEPLOY_CLOUDRUN_REGION", ""),
			ECSRegion:         getEnv("COOKER_DEPLOY_ECS_REGION", ""),
			ECSCluster:        getEnv("COOKER_DEPLOY_ECS_CLUSTER", ""),
			ECSExecutionRole:  getEnv("COOKER_DEPLOY_ECS_EXECUTION_ROLE", ""),
			ECSTaskRole:       getEnv("COOKER_DEPLOY_ECS_TASK_ROLE", ""),
			ECSSubnets:        getEnvCSV("COOKER_DEPLOY_ECS_SUBNETS", nil),
			ECSSecurityGroups: getEnvCSV("COOKER_DEPLOY_ECS_SECURITY_GROUPS", nil),
			FlyToken:          getEnv("COOKER_DEPLOY_FLY_TOKEN", ""),
			FlyRegion:         getEnv("COOKER_DEPLOY_FLY_REGION", ""),
			RenderToken:       getEnv("COOKER_DEPLOY_RENDER_TOKEN", ""),
			RenderOwnerID:     getEnv("COOKER_DEPLOY_RENDER_OWNER_ID", ""),
		},
		CloudInventory: CloudInventoryConfig{
			CacheTTL: getEnvDuration("COOKER_CLOUD_CACHE_TTL", 5*time.Minute),
			AWS: CloudAWSConfig{
				Enabled:         getEnvBool("COOKER_CLOUD_AWS_ENABLED", false),
				Region:          getEnv("COOKER_CLOUD_AWS_REGION", ""),
				AccessKeyID:     getEnv("COOKER_CLOUD_AWS_ACCESS_KEY_ID", ""),
				SecretAccessKey: getEnv("COOKER_CLOUD_AWS_SECRET_ACCESS_KEY", ""),
				SessionToken:    getEnv("COOKER_CLOUD_AWS_SESSION_TOKEN", ""),
			},
			GCP: CloudGCPConfig{
				Enabled:         getEnvBool("COOKER_CLOUD_GCP_ENABLED", false),
				ProjectID:       getEnv("COOKER_CLOUD_GCP_PROJECT_ID", ""),
				CredentialsJSON: getEnv("COOKER_CLOUD_GCP_CREDENTIALS_JSON", ""),
			},
		},
		Audit: AuditConfig{
			Enabled:     getEnvBool("COOKER_AUDIT_ENABLED", env.IsProduction()),
			Destination: getEnv("COOKER_AUDIT_DESTINATION", "stdout"),
			FilePath:    getEnv("COOKER_AUDIT_FILE_PATH", ""),
			DBRetention: getEnvDuration("COOKER_AUDIT_DB_RETENTION", 90*24*time.Hour),
		},
		Observability: ObservabilityConfig{
			MetricsEnabled: getEnvBool("COOKER_METRICS_ENABLED", false),
			TracingEnabled: getEnvBool("COOKER_TRACING_ENABLED", false),
			OTLPEndpoint:   getEnv("COOKER_OTLP_ENDPOINT", ""),
			OTLPInsecure:   getEnvBool("COOKER_OTLP_INSECURE", false),
			ServiceName:    getEnv("COOKER_SERVICE_NAME", "cooker"),
			ServiceVersion: getEnv("COOKER_SERVICE_VERSION", "dev"),
			MetricsPort:    getEnvInt("COOKER_METRICS_PORT", 0),
			MetricsHost:    getEnv("COOKER_METRICS_HOST", ""),
		},
		AppHealthInterval: getEnvDuration("COOKER_APP_HEALTH_INTERVAL", 30*time.Second),
		JobQueue: JobQueueConfig{
			Enabled:        getEnvBool("COOKER_JOBQUEUE_ENABLED", false),
			Workers:        getEnvInt("COOKER_JOBQUEUE_WORKERS", 4),
			WorkerIDPrefix: getEnv("COOKER_JOBQUEUE_WORKER_PREFIX", "cooker"),
		},
		Scheduler: SchedulerConfig{
			Enabled:   getEnvBool("COOKER_SCHEDULER_ENABLED", false),
			TickEvery: getEnvDuration("COOKER_SCHEDULER_TICK", 30*time.Second),
		},
		Governance: GovernanceConfig{
			URL:               getEnv("COOKER_GOVERNANCE_URL", ""),
			FailOpenEnvs:      getEnvCSV("COOKER_GOVERNANCE_FAIL_OPEN_ENVS", []string{"dev", "staging"}),
			BootstrapServices: getEnvCSV("COOKER_GOVERNANCE_BOOTSTRAP_SERVICES", []string{"governance"}),
			CallerToken:       getEnv("COOKER_GOVERNANCE_CALLER_TOKEN", ""),
			DelegateToken:     getEnv("COOKER_GOVERNANCE_DELEGATE_TOKEN", ""),
			BreakGlassEnabled: getEnvBool("COOKER_GOVERNANCE_BREAK_GLASS_ENABLED", false),
		},
		Triage: TriageConfig{
			Enabled: getEnvBool("COOKER_AI_TRIAGE_ENABLED", false),
			Model:   getEnv("COOKER_AI_TRIAGE_MODEL", ""),
			APIKey:  getEnv("ANTHROPIC_API_KEY", ""),
		},
		Feedback: FeedbackConfig{
			Repo:  getEnv("COOKER_FEEDBACK_GITHUB_REPO", "santapong/Cooker"),
			Token: getEnv("COOKER_FEEDBACK_GITHUB_TOKEN", ""),
		},
		License: licenseConfigFromEnv(),
	}
}

// licenseConfigFromEnv assembles LicenseConfig, unioning the plural
// COOKER_LICENSE_PUBLIC_KEYS (comma-separated, rotation-friendly) with the
// singular COOKER_LICENSE_PUBLIC_KEY (back-compat). The singular value is
// appended last and de-duplicated so a deployment that sets only one, the
// other, or both ends up with a clean key set.
func licenseConfigFromEnv() LicenseConfig {
	singular := getEnv("COOKER_LICENSE_PUBLIC_KEY", "")
	plural := getEnvCSV("COOKER_LICENSE_PUBLIC_KEYS", nil)

	keys := make([]string, 0, len(plural)+1)
	seen := make(map[string]struct{}, len(plural)+1)
	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" {
			return
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for _, k := range plural {
		add(k)
	}
	add(singular)

	return LicenseConfig{
		Key:        getEnv("COOKER_LICENSE_KEY", ""),
		PublicKey:  singular,
		PublicKeys: keys,
	}
}
