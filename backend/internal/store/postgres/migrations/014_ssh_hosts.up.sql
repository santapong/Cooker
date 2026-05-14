-- 014_ssh_hosts.up.sql
-- Add SSH-host columns to `hosts` for the Dokploy/Coolify-style
-- remote deploy target (Thread 1 of the 2026-05 plan). These columns
-- are NULL/empty for existing docker / kubernetes hosts; they are
-- populated when Kind='ssh-docker'. The private key body itself
-- never lands here — only the secrets.Manager reference does.
--
-- ssh_known_host_key carries the host's public key in ssh-pubkey
-- format (e.g. "ssh-ed25519 AAAA..."). It's empty on Create when
-- TOFU mode is enabled and gets pinned on first connect; subsequent
-- connects refuse on mismatch unless ssh_strict_host_key=false.
-- Config.Validate refuses ssh_strict_host_key=false in production.

ALTER TABLE hosts
    ADD COLUMN IF NOT EXISTS ssh_endpoint         TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ssh_user             TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ssh_port             INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS ssh_private_key_ref  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ssh_known_host_key   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ssh_strict_host_key  BOOLEAN NOT NULL DEFAULT TRUE;
