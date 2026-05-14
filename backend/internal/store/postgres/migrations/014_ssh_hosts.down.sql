-- 014_ssh_hosts.down.sql -- reverse 014_ssh_hosts.up.sql.

ALTER TABLE hosts
    DROP COLUMN IF EXISTS ssh_endpoint,
    DROP COLUMN IF EXISTS ssh_user,
    DROP COLUMN IF EXISTS ssh_port,
    DROP COLUMN IF EXISTS ssh_private_key_ref,
    DROP COLUMN IF EXISTS ssh_known_host_key,
    DROP COLUMN IF EXISTS ssh_strict_host_key;
