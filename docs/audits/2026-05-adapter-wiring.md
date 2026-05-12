# Adapter Wiring Audit (2026-05)

**Scope:** `selectBuilder`, `selectPusher`, `selectDeployer`, `selectSecretsManager` in
`backend/internal/server/server.go`; the rate-limiter and WebSocket backend switches in
`server.go` + `router.go`; deploy-target self-registration in
`backend/internal/server/deploytargets.go`.
**Method:** Static reading of all `selectXxx` functions and `Config.Validate`.
Line spans cited at the time of reading (`server.go` is at commit on branch
`claude/research-adapter-wiring`; `config.go` is the same tree).
**Verdict:** Three of five `selectXxx` functions boot successfully on a misspelled
backend name, silently delivering a no-op adapter. One secrets-manager error message
is undecorated inside `selectSecretsManager`. The production boot guard
(`Config.Validate`) has a peer gap for the `docker` pusher that matches the
documented gap it already closes for the `docker` builder.

---

## Finding 1 — Silent `default` in `selectBuilder`, `selectPusher`, and `selectDeployer`

**Severity: High**

**File/lines:**
- `backend/internal/server/server.go:444-470` — `selectBuilder`
- `backend/internal/server/server.go:472-481` — `selectPusher`
- `backend/internal/server/server.go:483-494` — `selectDeployer`

**Description.**
All three functions fall through to an implicit `default:` case that returns a `Noop{}`
adapter rather than an error. An operator who sets `COOKER_BUILDER=kanika` (typo of
`kaniko`) gets a silent no-op builder: the server boots cleanly, every build stage
immediately succeeds without producing any image, and no log line names the cause. The
same silent swallow applies to `COOKER_PUSHER` and `COOKER_DEPLOYER` typos.

`selectSecretsManager` does the right thing: its `default:` returns
`fmt.Errorf("unknown secrets backend %q", ...)` at boot. The other three should mirror
that. `selectBuilder` already has the right `(builder.Builder, error)` signature wired to
`New()` at line 183-187. `selectPusher` and `selectDeployer` return bare values, so the
fix requires a small signature change and call-site updates at lines 190-191.

**Recommended fix.**
Replace `default: return <Noop>{}, nil` with
`default: return nil, fmt.Errorf("unknown %s backend %q", <name>, kind)` in each
function. Mirror the `selectSecretsManager` pattern exactly. For `selectPusher` and
`selectDeployer`, also change the return type to `(T, error)` and propagate the error
in `New()`.

---

## Finding 2 — `selectWSHubBackend` and `selectWSTicketStore` have silent `default` cases that mask typos

**Severity: Medium**

**File/lines:**
- `backend/internal/server/server.go:129-143` — `wsHub` switch
- `backend/internal/server/server.go:146-152` — `wsTickets` switch
- `backend/internal/server/router.go:55-63` — rate-limiter switch

**Description.**
All three in-process / Redis backend pickers use `default:` to mean "use the memory
backend". A typo in `COOKER_WS_HUB_BACKEND` or `COOKER_WS_TICKET_BACKEND` boots with
in-memory on every replica, silently defeating ticket sharing and rate-limit state across
replicas. `Config.Validate` at `config.go:459-474` catches this *only when
`COOKER_REPLICA_COUNT > 1`*; operators who leave `ReplicaCount=1` (the default) and
scale later get no warning. The rate-limiter switch in `router.go:55-63` is a bare
`switch { case ... }` — not `switch cfg.RateLimit.Backend` — so it cannot detect an
unknown string at all; it falls to the in-memory `default` regardless.

**Recommended fix.**
For `wsHub` and `wsTickets`, validate the configured backend value against the
known set `{"memory", "redis"}` before the switch and return an error from `New()` on an
unknown value. For the rate-limiter switch, convert it to `switch cfg.RateLimit.Backend`
and add a case for unknown values. Alternatively, add a `Config.Validate` check on all
three backend strings unconditionally (not only when `ReplicaCount > 1`).

---

## Finding 3 — Inconsistent error wrapping across `selectXxx` functions

**Severity: Low**

**File/lines:**
- `backend/internal/server/server.go:96-100` — `selectSecretsManager` call site
- `backend/internal/server/server.go:501-547` — `selectSecretsManager` body
- `backend/internal/server/server.go:444-470` — `selectBuilder` body
- `backend/internal/server/server.go:183-187` — `selectBuilder` call site

**Description.**
`selectBuilder` returns raw errors from `builder.NewKaniko` and `builder.NewBuildah`
(e.g. `"kaniko: kubeconfig: ..."`) without adding a "select builder:" prefix. The call
site in `New()` at line 186 wraps it as `fmt.Errorf("builder: %w", err)`, so the final
message is `"builder: kaniko: ..."` — acceptable.

Inside `selectSecretsManager`, some error paths are wrapped with a package-level prefix
(e.g. `fmt.Errorf("keepsave backend requires ...")`), while the `vault` case constructs
an error with no prefix beyond what `vault.New` returns. The outer call site at line 99
wraps it as `fmt.Errorf("secrets: %w", err)`, so vault errors read `"secrets: vault: ..."`,
which is fine, but the keepsave validation error reads `"secrets: keepsave backend
requires ..."` without the consistent `"secrets: keepsave: ..."` prefix used by other
backends.

Compared to `selectSecretsManager:508` (`"keepsave backend requires ..."`) vs
`selectSecretsManager:506-510` which has no "select secrets manager: " prefix inside,
the outer wrap brings them all to `"secrets: <raw message>"`. The inconsistency is that
keepsave's inline validation messages (`"keepsave backend requires COOKER_SECRETS_..."`
at line 506) differ in style from vault's at line 514 (`"vault backend requires
COOKER_SECRETS_VAULT_ADDR"`) — one says "keepsave backend", the other says "vault backend".
Neither follows the `"<package>: <verb>: %w"` convention from `CLAUDE.md`.

**Recommended fix.**
Standardise all validation error strings inside `selectSecretsManager` to
`fmt.Errorf("select secrets manager: keepsave: ...")` and
`fmt.Errorf("select secrets manager: vault: ...")`. Then the outer `"secrets: %w"` wrap
produces greppable `"secrets: select secrets manager: keepsave: ..."`. For `selectBuilder`
the existing pattern is already reasonable; add `"select builder: "` inside the function
so it doesn't depend on the call-site wrap.

---

## Finding 4 — `Config.Validate` does not gate `COOKER_PUSHER=docker` in production

**Severity: High**

**File/lines:**
- `backend/internal/config/config.go:452-454` — builder docker gate
- `backend/internal/config/config.go:359-479` — full `Validate` body
- `backend/internal/pusher/docker.go` — `DockerSock` pusher

**Description.**
`Config.Validate` (line 452) already refuses `COOKER_BUILDER=docker` in production with
the message "COOKER_BUILDER=docker is unsafe in production (host docker.sock RCE-to-host)".
This guard exists because the builder bind-mounts `/var/run/docker.sock`. The pusher's
`DockerSock` implementation (`pusher/docker.go`) also shells out to the `docker` CLI,
which implicitly uses the mounted socket for `docker push`. An operator who reads the
builder warning, switches to `COOKER_BUILDER=kaniko`, but leaves `COOKER_PUSHER=docker`
still pushes through the host daemon's socket without any boot-time warning. The
RCE-to-host socket path is still open via the pusher even after the builder guard passes.

`Config.Validate` has no corresponding check for `COOKER_PUSHER=docker`, so a production
deployment that uses Kaniko to build but Docker to push boots cleanly, exposing the same
docker.sock surface the builder guard was meant to close.

**Recommended fix.**
Add to `Config.Validate`:
```go
if c.PusherBackend == "docker" {
    problems = append(problems, "COOKER_PUSHER=docker is unsafe in production "+
        "(host docker.sock); use crane instead")
}
```
Mirror the wording and placement of the existing builder check at `config.go:452-454`.

---

## Finding 5 — `deploytargets.go` registration failures are non-fatal and silently swallowed

**Severity: Low**

**File/lines:**
- `backend/internal/server/deploytargets.go:19-28` — `tryRegister` closure

**Description.**
`registerDeployTargets` wraps every `deploytarget.Register` call in a `tryRegister`
closure that logs a warning on failure and returns — it does not propagate errors out of
`registerDeployTargets`. The caller at `server.go:304` ignores the return value
(`registerDeployTargets(cfg.DeployTargets)`). If a deploy-target adapter returns an error
from `Register` for any reason other than `ErrDuplicateKind`, the operator sees a
`slog.Warn` line and the target is silently absent: deploy attempts to that target will
fail at runtime with `ErrUnavailable` rather than at boot.

In practice the current four adapters (`cloudrun`, `ecs`, `flyio`, `render`) only error
from `Register` via `ErrDuplicateKind` — but future adapters that perform a
credential-validation ping inside their constructor (the recommended pattern) will return
an error that is silently eaten here.

**Recommended fix.**
Return an `error` from `registerDeployTargets` and propagate it from `New()`:
```go
func registerDeployTargets(cfg config.DeployTargetsConfig) error {
    ...
    if err := deploytarget.Register(t); err != nil && !errors.Is(err, deploytarget.ErrDuplicateKind) {
        return fmt.Errorf("deploytarget %s: %w", name, err)
    }
    ...
}
```
Call sites in `server.go:304` become:
```go
if err := registerDeployTargets(cfg.DeployTargets); err != nil {
    cleanup()
    return nil, fmt.Errorf("deploy targets: %w", err)
}
```

---

## Severity summary

| # | Issue | Severity | File:lines |
|---|---|---|---|
| 1 | Silent `default` in `selectBuilder`, `selectPusher`, `selectDeployer` — typo boots with noop | **High** | `server.go:444-494` |
| 2 | `Config.Validate` has no `COOKER_PUSHER=docker` production guard (peer to the builder guard) | **High** | `config.go:452-454` |
| 3 | Silent `default` in wsHub, wsTickets, rate-limiter backend pickers — typo silently uses memory | **Medium** | `server.go:129-152`, `router.go:55-63` |
| 4 | Inconsistent error-wrapping style inside `selectSecretsManager` | **Low** | `server.go:501-547` |
| 5 | `registerDeployTargets` swallows non-duplicate registration errors | **Low** | `deploytargets.go:19-28` |

---

## Cross-reference: `Config.Validate` boot-guard coverage

| Backend | `Validate` guard exists? | Notes |
|---|---|---|
| `COOKER_BUILDER=docker` | Yes — `config.go:452` | Refuses in production |
| `COOKER_BUILDER=kaniko/buildah` | No | PVC / namespace absence is a runtime error only |
| `COOKER_PUSHER=docker` | **No** | Finding 4 above — peer gap to builder guard |
| `COOKER_DEPLOYER=kubectl/clientgo` | No | Kubeconfig absence surfaces at runtime |
| `COOKER_SECRETS_BACKEND=keepsave` | Yes — `config.go:395-405` | URL must be https:// |
| `COOKER_SECRETS_BACKEND=vault` | Partial — `config.go:407-409` | Addr checked; Token not validated |
| `COOKER_SECRETS_BACKEND=aws` | None | Region auto-detected; no explicit gate |
| `COOKER_SECRETS_BACKEND=gcp` | Yes — `config.go:413-415` | ProjectID required |
| `COOKER_WS_HUB/TICKET/RATE_LIMIT_BACKEND` | Only if `ReplicaCount>1` | Finding 3 above |

**See also:** [`dag-performance.md`](dag-performance.md),
[`vulnerabilities-and-chains.md`](vulnerabilities-and-chains.md), and
[`launch-readiness.md`](launch-readiness.md) for related stability and security findings.
