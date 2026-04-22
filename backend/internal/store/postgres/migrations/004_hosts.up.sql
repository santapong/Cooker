-- Managed hosts (Docker or Kubernetes). Reachability selects
-- whether Cooker dials the host directly or routes through the
-- Tailscale tailnet (see internal/transport/tsnet).
CREATE TABLE IF NOT EXISTS hosts (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    kind            TEXT NOT NULL,           -- docker|kubernetes
    reachability    TEXT NOT NULL DEFAULT 'direct', -- direct|tailnet
    docker_endpoint TEXT DEFAULT '',
    kubeconfig_ref  TEXT DEFAULT '',
    tailnet_ip      TEXT DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
