-- 021_settings_configs.up.sql
--
-- Real persistence for the Settings page's registry + cluster CRUD
-- (audit finding HS26-05-04). Before this, handler/registry.go's
-- ListRegistryConfigs / AddRegistryConfig / DeleteRegistryConfig /
-- ListClusterConfigs / AddClusterConfig were stubs that returned a
-- synthesized 201 and an empty list — the operator added a registry,
-- saw a success toast, refreshed, and it was gone.
--
-- One migration covers both tables. They mirror the `hosts` table
-- shape: secret material (the registry auth password, the cluster
-- kubeconfig body) NEVER lands in these columns — only the
-- secrets.Manager reference does (password_ref / kubeconfig_ref),
-- exactly like hosts.ssh_private_key_ref. The service layer
-- (service.RegistryConfigService / ClusterConfigService) writes the
-- bytes through secrets.Manager and stamps the ref here.

CREATE TABLE IF NOT EXISTS registry_configs (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    url          TEXT NOT NULL,
    username     TEXT NOT NULL DEFAULT '',
    -- secrets.Manager reference for the auth password/token; empty for
    -- anonymous registries. The password body never lands here.
    password_ref TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cluster_configs (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    context        TEXT NOT NULL DEFAULT '',
    -- secrets.Manager reference for the kubeconfig body; empty when the
    -- operator only recorded a context name. The kubeconfig never lands here.
    kubeconfig_ref TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
