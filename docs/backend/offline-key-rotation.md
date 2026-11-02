# Offline encryption-key rotation

This tool rewraps Cooker-managed database ciphertext with a new 32-byte AES key.
It preserves configuration generations, deployment authority and queued payloads.
It does not replace provider tokens or rotate values in an external secrets manager.

## Scope and prerequisites

The inventory covers every environment secret map (including internal, disabled
and orphaned records), App webhook secret, source connection credential, Vercel
connection credential, Neon management credential and encrypted notification config. Existing plaintext
notification configs are counted and left unchanged; their contents never enter
the ciphertext backup. They require a separate, explicit configuration update to
become encrypted.

### Neon schema042 compatibility

Schema042 adds a sixth managed ciphertext family, `neon_connections.credential`.
Use a matching six-family executable. It locks and inventories that table,
refuses active Neon refresh leases, and preserves connection versions/snapshots
while rewrapping. The new credential envelope retains its purpose, connection
and organization binding inside authenticated encryption.

The migration also adds `vercel_credential_rewrap_guard`. Ordinary Vercel key
replacement advances the logical version and remains allowed. A ciphertext-only
rewrap with an unchanged version requires the transaction-local six-family
marker, set only after the current tool's offline/schema qualification. Older
five-family tools encounter a second trigger in their existing schema check and
refuse before backup/write; do not remove that guard to make an older tool run.
The marker is compatibility coordination, not authorization against a database
operator. Its exact enabled trigger/function definitions are checked by the
current executable.

Down042 refuses any remaining Neon connection, including paused or unselected
ones. Disconnecting locally does not delete provider resources. A five-family
backup can only match current recovery state when no new Neon rows exist;
otherwise the inventory is drift. Keep schema downgrade, executable rollback,
credential replacement and key rewrap as separate reviewed actions.

Implementation and fixture source are prepared. Actual PostgreSQL/CLI recovery
acceptance for this sixth-family change remains pending; the default Neon unit
pass does not establish it. No operator key rotation is authorized by this guide.

First activate a build that can read both legacy and `tagged-v1` ciphertext.
Default writes remain legacy for staged compatibility. Explicitly configure
`COOKER_SECRET_CIPHER_FORMAT=tagged-v1` when enabling tagged writes. A tagged
record cannot be read by an older build that lacks this reader, including the
previously tested compatible fallback. Its earlier rollback evidence does not
establish compatibility with rotated storage.

Before any mutation, stop **all** Cooker API, worker, poller and notification
processes for this database, resolve active work, and prevent another instance
from starting during maintenance. The tool checks other database clients and
active work and holds exclusive table locks. Those checks cannot prevent an
operator starting a new process after the check; this is an offline procedure.

Keep a full database backup and the original key in separate secure storage.
The tool creates a new private ciphertext backup with authenticated record
bindings and both before/after ciphertext; it contains neither key nor plaintext.
It is not a replacement for the full database backup. Existing files are never
overwritten. File and directory synchronization complete before database writes.

## Private input and commands

The only command argument is the operation. Supply a maximum 64 KiB JSON object
through a private stdin pipe or protected temporary input file, never through
arguments, shell history or an HTTP/browser form:

```text
databaseUrl: exact maintenance database URL
oldKey: original base64-encoded 32-byte key
newKey: replacement base64-encoded 32-byte key
backupPath: absolute path to a new private ciphertext-backup file
actor: operator audit label (reported by the caller, not authenticated identity)
offline: true for rotate and restore
```

`inventory` needs `databaseUrl` and `oldKey`; the other operations also need
`newKey` and `backupPath`. Preserve both keys independently throughout recovery.

```sh
cooker key-rotation inventory < /run/private/rotation-input.json
cooker key-rotation rotate    < /run/private/rotation-input.json
cooker key-rotation status    < /run/private/rotation-input.json
cooker key-rotation restore   < /run/private/rotation-input.json
```

The input file, if used, must be private and ephemeral. Capture the result JSON
in the operator audit record. It contains the operation, caller-supplied actor,
rotation ID, key identifiers, family counts and state; errors are fixed messages.
The tool never prints database credentials, encryption keys, plaintext values or
raw database/provider errors.

The two-minute operation deadline includes reading private input. SIGINT/SIGTERM
cancels an incomplete stdin read and closes that input, returning a fixed refusal
instead of waiting indefinitely for the feeder to finish. Rejected input bytes are
cleared before return; this does not guarantee erasure of every runtime string copy.

## Recovery states

| State | Meaning | Action |
| --- | --- | --- |
| `before` | All records match the authenticated original inventory | No committed rewrap is observed; diagnose the failed attempt before preparing a new backup |
| `after` | All records match the authenticated replacement inventory | Rotation is observed; start only a compatible reader with the replacement key |
| `mixed` | Some records match before and others after | Refuse automatic rotation/restore; investigate the complete database |
| `drift` | Rows, metadata/generations or ciphertext differ from the backup | Refuse restore; use a reviewed recovery procedure/full backup |
| `no_encrypted_values` | Nothing managed by this key needs rewrapping | No ciphertext backup or mutation is created |
| `commit_uncertain` | The database commit acknowledgement was lost or failed | Run `status` with the same authenticated backup; never blindly retry |

`restore` accepts only the complete expected `after` state. It restores exact
original ciphertext representations and preserves logical versions. New records,
deletions, changed versions/metadata or replaced values prevent stale restoration.
Already restored records return `already_restored` without writes. A successful
restore requires the original key and a reader compatible with the original
representations. If the original inventory already contained tagged records,
restoring it does not make an older legacy-only executable compatible.

Legacy plaintext notification contents are excluded from the backup. Their ID,
version and remaining metadata participate in drift checks; their config is never
written by this tool. Out-of-band SQL that changes config while bypassing the
version trigger is outside that observation boundary.

## Atomicity and limits

All five families use one transaction. Environment/App/source/Vercel generations,
timestamps and referenced authority remain unchanged. The exact notification
version trigger is verified, disabled only within that locked transaction, and
restored before commit; rollback also restores its state. Notification versions
and sequence values therefore remain unchanged by the rewrap.

Malformed ciphertext, a wrong key, an unknown notification envelope, an altered
trigger, unsafe backup or unsupported schema fails before commit. The tool bounds
inventory/backup size and database lock/operation time. There is no automatic
provider mutation, retry, migration, online keyring transition or server activation.

Local acceptance is recorded separately in
`/mnt/data/cooker-roadmap-20261002/key-rotation/`. The actual CLI campaign passed
inventory/rotation/status/exact restore through fourteen CLI processes and two
PostgreSQL process restarts on the same owned volume, including unsafe input,
stale backup and original-key/ciphertext checks. It uses synthetic retained SQL
jobs/outbox, not genuinely admitted deployments. A separate six-process wire-fault
campaign suppressed COMMIT before submission and after confirmed backend commit.
Both returned `commit_uncertain`; fresh status correctly distinguished `before`
and `after`, with exact original recovery.

A separate seven-CLI campaign uses jobs genuinely admitted by Cooker's
PostgreSQL App/native producers and their production enqueue callbacks. Six
fresh helper processes verified the original selected Dockerfile keys, explicit
empty selection, native stage keys and granted registry credentials before and
after rewrap, after a PostgreSQL restart and after exact restore. Recorded
payloads, receipt retries, grants/incarnations and logical generations remain
unchanged; the wrong key refuses resolution. These jobs stay unclaimed, so this
proves accepted-plan resolution rather than deployment execution.

That campaign also verifies a real `ENOSPC` write on an exhausted owned 64 KiB
tmpfs, then an official CLI backup refusal with its new mode-0600 file retained
empty. A fresh accepted-plan observation confirms exact pre-rotation ciphertext
and business state afterward. It does not inject a file/directory fsync error.
Other crash windows, fsync faults, deployment execution, executable fallback and
owner installation acceptance remain separate; restart alone does not prove them.
