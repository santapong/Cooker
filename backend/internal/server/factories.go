package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/santapong/cooker/internal/builder"
	"github.com/santapong/cooker/internal/cloudinventory"
	cloudaws "github.com/santapong/cooker/internal/cloudinventory/aws"
	cloudgcp "github.com/santapong/cooker/internal/cloudinventory/gcp"
	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/deployer"
	"github.com/santapong/cooker/internal/pusher"
	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/secrets/awsm"
	"github.com/santapong/cooker/internal/secrets/database"
	"github.com/santapong/cooker/internal/secrets/gcpsm"
	"github.com/santapong/cooker/internal/secrets/keepsave"
	"github.com/santapong/cooker/internal/secrets/vault"
	"github.com/santapong/cooker/internal/stagerunner"
	"github.com/santapong/cooker/internal/store"
)

func selectBuilder(kind string, k8s config.KubernetesConfig) (builder.Builder, error) {
	switch kind {
	case "docker":
		return builder.NewDockerSock(), nil
	case "buildkit":
		return builder.NewBuildKit(""), nil
	case "kaniko":
		return builder.NewKaniko(builder.KanikoConfig{
			Kubeconfig:     k8s.Kubeconfig,
			Namespace:      k8s.Namespace,
			Image:          k8s.KanikoImage,
			ServiceAccount: k8s.KanikoServiceAccount,
			ContextPVC:     k8s.KanikoContextPVC,
		})
	case "buildah":
		return builder.NewBuildah(builder.BuildahConfig{
			Kubeconfig:     k8s.Kubeconfig,
			Namespace:      k8s.Namespace,
			Image:          k8s.BuildahImage,
			ServiceAccount: k8s.BuildahServiceAccount,
			ContextPVC:     k8s.BuildahContextPVC,
			StorageDriver:  k8s.BuildahStorageDriver,
		})
	default:
		return builder.Noop{}, nil
	}
}

// selectStageRunner builds the container runtime for Test/Custom stages
// from COOKER_STAGE_RUNNER. It mirrors selectBuilder: "kube" submits a
// one-shot Job to the configured cluster (reusing the same K8s config as
// the Kaniko/Buildah builders), "docker" shells `docker run`, and the
// default ("noop" / unset) does not start a container — so a dev/test
// install exercises Test/Custom stages without a runtime. An unknown value
// falls through to noop rather than failing boot, matching selectBuilder.
func selectStageRunner(kind string, k8s config.KubernetesConfig) (stagerunner.Runner, error) {
	switch kind {
	case "docker":
		return stagerunner.NewDockerRun(), nil
	case "kube":
		return stagerunner.NewKube(stagerunner.KubeConfig{
			Kubeconfig:     k8s.Kubeconfig,
			Namespace:      k8s.Namespace,
			ServiceAccount: k8s.KanikoServiceAccount,
		})
	default:
		return stagerunner.Noop{}, nil
	}
}

func selectPusher(kind string) pusher.Pusher {
	switch kind {
	case "docker":
		return pusher.NewDockerSock()
	case "crane":
		return pusher.NewCrane()
	default:
		return pusher.Noop{}
	}
}

func selectDeployer(kind, kubeconfig string) deployer.Deployer {
	switch kind {
	case "kubectl":
		d := deployer.NewKubectl()
		d.Kubeconfig = kubeconfig
		return d
	case "clientgo":
		return deployer.NewClientGo(kubeconfig)
	default:
		return deployer.Noop{}
	}
}

func selectSecretsManager(cfg *config.Config, st *store.Store, codec *crypto.Codec) (secrets.Manager, error) {
	ctx := context.Background()
	switch cfg.SecretsBackend {
	case "keepsave":
		if cfg.KeepSave.URL == "" || cfg.KeepSave.ProjectID == "" || cfg.KeepSave.APIKey == "" {
			return nil, fmt.Errorf("keepsave backend requires COOKER_SECRETS_KEEPSAVE_URL, _PROJECT_ID, _API_KEY")
		}
		client := keepsave.NewClient(cfg.KeepSave.URL, cfg.KeepSave.APIKey)
		slog.Info("secrets backend selected", "backend", "keepsave", "url", cfg.KeepSave.URL, "project", cfg.KeepSave.ProjectID)
		return keepsave.New(client, st.Environments, cfg.KeepSave.ProjectID), nil
	case "vault":
		if cfg.Vault.Addr == "" {
			return nil, fmt.Errorf("vault backend requires COOKER_SECRETS_VAULT_ADDR")
		}
		slog.Info("secrets backend selected", "backend", "vault", "addr", cfg.Vault.Addr, "mount", cfg.Vault.Mount)
		return vault.New(vault.Config{
			Address: cfg.Vault.Addr,
			Token:   cfg.Vault.Token,
			Mount:   cfg.Vault.Mount,
			Prefix:  cfg.Vault.Prefix,
		})
	case "aws":
		slog.Info("secrets backend selected", "backend", "aws-secrets-manager", "region", cfg.AWSSecrets.Region, "prefix", cfg.AWSSecrets.Prefix)
		return awsm.New(ctx, awsm.Config{
			Region: cfg.AWSSecrets.Region,
			Prefix: cfg.AWSSecrets.Prefix,
		})
	case "gcp":
		if cfg.GCPSecrets.ProjectID == "" {
			return nil, fmt.Errorf("gcp backend requires COOKER_SECRETS_GCP_PROJECT_ID")
		}
		slog.Info("secrets backend selected", "backend", "gcp-secret-manager", "project", cfg.GCPSecrets.ProjectID)
		return gcpsm.New(ctx, gcpsm.Config{
			ProjectID: cfg.GCPSecrets.ProjectID,
			Prefix:    cfg.GCPSecrets.Prefix,
		})
	case "database", "":
		if codec == nil || !codec.Active() {
			slog.Warn("secrets backend selected", "backend", "database", "codec", "inactive", "note", "secret endpoints will return 503 until COOKER_SECRET_KEY is set")
			return nil, nil
		}
		slog.Info("secrets backend selected", "backend", "database", "codec", "AES-GCM")
		return database.New(st.Environments, codec), nil
	default:
		return nil, fmt.Errorf("unknown secrets backend %q", cfg.SecretsBackend)
	}
}

// newCloudInventory builds the read-only cloud inventory service from
// COOKER_CLOUD_* config (OR-2). It constructs a provider for each
// enabled cloud (AWS / GCP) and returns a Service that fans out to them
// with a TTL cache. When no provider is enabled it returns a Service
// with zero providers — Enabled() is false and the handlers return an
// empty, enabled=false payload.
//
// A provider whose SDK client fails to construct (bad region, malformed
// credentials JSON) fails the boot, matching the fail-fast posture of
// the other adapters — a misconfigured cloud should surface at startup,
// not on the first panel load. The construction touches no cloud API.
func newCloudInventory(ctx context.Context, cfg config.CloudInventoryConfig) (*cloudinventory.Service, error) {
	var providers []cloudinventory.Provider
	if cfg.AWS.Enabled {
		p, err := cloudaws.New(ctx, cloudaws.Config{
			Region:          cfg.AWS.Region,
			AccessKeyID:     cfg.AWS.AccessKeyID,
			SecretAccessKey: cfg.AWS.SecretAccessKey,
			SessionToken:    cfg.AWS.SessionToken,
		})
		if err != nil {
			return nil, fmt.Errorf("cloud inventory: aws: %w", err)
		}
		providers = append(providers, p)
		slog.Info("cloud inventory provider enabled", "provider", "aws", "region", cfg.AWS.Region)
	}
	if cfg.GCP.Enabled {
		p, err := cloudgcp.New(ctx, cloudgcp.Config{
			ProjectID:       cfg.GCP.ProjectID,
			CredentialsJSON: cfg.GCP.CredentialsJSON,
		})
		if err != nil {
			return nil, fmt.Errorf("cloud inventory: gcp: %w", err)
		}
		providers = append(providers, p)
		slog.Info("cloud inventory provider enabled", "provider", "gcp", "project", cfg.GCP.ProjectID)
	}
	return cloudinventory.New(providers, cloudinventory.WithTTL(cfg.CacheTTL)), nil
}
