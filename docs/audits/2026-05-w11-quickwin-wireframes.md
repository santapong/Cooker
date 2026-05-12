# W11 P2 frontend quick-win wireframes (2026-05)

Read-only research deliverable. Covers three W11 P2 items that can ship without backend changes, plus a follow-up scan of the PR #38 bundle-split. Each wireframe is ASCII art with a behaviour paragraph and the Zustand / API call needed. Task B is the PR #38 drift analysis.

Persona citations: `docs/audits/W11-user-journeys.md` §Indie.

---

## Task A — Text wireframes

### A1 — Webhook URL on `AppDetailPage` (W11 §Indie step 5)

The indie hacker enables AutoDeploy but has no in-product answer to "what URL do I paste into GitHub?" They have to read `docs/architecture.md`. The fix: surface the webhook receiver URL next to the AutoDeploy toggle in the left-hand info card.

The backend exposes the receiver at `POST /api/v1/webhooks/github` (see `backend/internal/server/router.go:191`). The URL is deterministic: `<origin>/api/v1/webhooks/github`. No new API field is needed; the frontend derives it from `window.location.origin`. The `app.hasWebhook` flag (already in `AppModel`) drives the copy-button's enabled state.

**Wireframe — left-hand `Card` (info column, 320 px wide), GitHub webhook section:**

```
┌─────────────────────────────────────────────┐
│ GITHUB WEBHOOK          ─────────────────── │
│                                             │
│  [• webhook]  [• auto-deploy]               │  ← existing Pills (app.hasWebhook, app.autoDeploy)
│                                             │
│  WEBHOOK ENDPOINT                           │
│ ┌───────────────────────────────────────┐   │
│ │ https://app.example.com/api/v1/       │   │  ← monospace Input (readOnly)
│ │ webhooks/github                       │   │
│ └───────────────────────────────────────┘   │
│  [ Copy URL ]                               │  ← Btn kind="secondary", icon="copy"
│                                             │
│  ─── or ──────────────────────────────────  │
│                                             │
│  [ Rotate webhook secret ]                  │  ← existing button (unchanged)
└─────────────────────────────────────────────┘
```

**Behaviour.** The webhook URL is rendered unconditionally (even before a secret is set) so the operator can paste the URL into GitHub while they configure the secret. The `Input` is `readOnly`; clicking "Copy URL" calls `navigator.clipboard.writeText(url)` and fires a `pushToast({ kind: 'success', message: 'Copied!' })`. The "Copy URL" button is never disabled — the URL is always valid regardless of `hasWebhook`.

**Zustand / store call.** No store needed. The URL is `${window.location.origin}/api/v1/webhooks/github`, derived inline. The toast uses `useToastStore((s) => s.push)` which `AppDetailPage` already imports. The existing `app.hasWebhook` from local component state drives the Pills — no new fetch.

**Existing atoms used.** `Card`, `SectionLabel`, `Field`, `Input` (readOnly variant), `Btn` (kind="secondary"), `Pill`. All imported from `../components/ui/atoms`.

---

### A2 — Deployed URL on `AppDetailPage` (W11 §Indie step 6)

After a successful deploy the indie hacker wants to open their app. The `Status.URL` from the deploy target is not surfaced anywhere in the UI. It sits inside the run details stream but is not explicitly extracted or linked.

`AppDeployResponse` (from `frontend/src/types/app.ts`) does not currently carry a `url` field. The deploy-target URL lives in the backend's `DeployTarget.Status.URL`. For this wireframe, the assumption is that the backend would expose it either on the `AppModel` (e.g. `deployedURL?: string`) or on `AppDeployResponse`. The wireframe shows it on the deploy-history card inside the log panel — the natural place once a run completes.

**Wireframe — right-hand `Card` (log panel), header row after deploy completes:**

```
┌─────────────────────────────────────────────────────────────────────┐
│  Build & deploy logs          [run 3a7f1b2c]  [• streaming]        │  ← existing header
│ ─────────────────────────────────────────────────────────────────── │
│                                                                     │
│  LAST DEPLOY                                                        │
│ ┌─────────────────────────────────────────────────────────────────┐ │
│ │ Status   [• deployed]    Run  3a7f1b2c                          │ │
│ │ URL      https://app.example.com          [ Visit ↗ ]           │ │  ← anchor Btn kind="ghost"
│ └─────────────────────────────────────────────────────────────────┘ │
│                                                                     │
│ <log stream scrollback>                                             │
└─────────────────────────────────────────────────────────────────────┘
```

**Behaviour.** The "Last deploy" summary card appears between the log panel header and the `<pre>` block only when `lastDeploy` is non-null. The `url` field renders as a truncated monospace span with a "Visit" anchor (`<a href={url} target="_blank" rel="noopener noreferrer">`). When no URL is present (e.g., docker-host targets with no ingress), the row is omitted. Polling the existing `appsApi.get(id)` every 30 s (already in place) would refresh `deployedURL` without extra traffic.

**Zustand / store call.** `AppDetailPage` already holds `lastDeploy: AppDeployResponse | null` in local state. A new optional `url?: string` field on `AppDeployResponse` (or a separate `app.deployedURL?: string` on `AppModel`) would feed this. The store call on the view layer is simply reading `lastDeploy.url ?? app.deployedURL`. No new store action; the API shape change is owned by `cooker-frontend-state` + backend.

**Existing atoms used.** `Card` (inner summary card), `Field`, `Pill` (status tone via `statusTone()`), `SectionLabel`. The "Visit" button is a plain `<a>` styled to match `Btn kind="ghost"` rather than a real `Btn` (anchor semantics for `target="_blank"`).

---

### A3 — Empty-state CTAs on Apps / Pipelines / Environments (W11 §Indie step 2)

A fresh install shows three empty list pages. Currently:

- `AppsPage`: shows `EmptyServices` (a local component with "Nothing in the kitchen yet" + "Connect a repo" `Btn`). This is close but the copy does not narrate the onboarding sequence.
- `PipelinesPage`: uses the shared `EmptyState` atom with "No pipelines yet." and a "Create pipeline" `Btn`. Has no secondary link.
- `EnvironmentsPage`: uses `EmptyState` with "No environments yet." and a "Seed dev / staging / prod" `Btn`. Has no secondary link.

The fix is the same shape on all three: a two-action `EmptyState` with a primary CTA and a secondary "Read the user guide" anchor.

**Wireframe — shared `EmptyState` pattern (all three pages):**

```
┌──────────────────────────────────────────────────────────────────────┐
│                                                                      │
│                  Nothing cooking yet.                                │  ← serif h2, t.text
│                                                                      │
│    Connect Cooker to a GitHub repo and we'll handle                  │  ← body copy, t.textSoft
│    build → ship → run end to end.                                    │
│                                                                      │
│          [ + Create your first App ]  [ Read the user guide ↗ ]     │  ← primary Btn + ghost anchor
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

Per-page copy variants (title / body / CTA):

| Page | Title | Body | Primary CTA |
|---|---|---|---|
| `AppsPage` | "Nothing cooking yet." | "Connect Cooker to a GitHub repo and we'll handle build → ship → run end to end." | "Create your first App" → `/apps/new` |
| `PipelinesPage` | "No pipelines yet." | "Draw your CI/CD graph visually — drag nodes, wire steps, watch each run stream live." | "Create your first Pipeline" → `pipelineApi.create(...)` then navigate |
| `EnvironmentsPage` | "No environments yet." | "Set up dev → staging → production so deploys have a conveyor belt to roll along." | "Seed dev / staging / prod" (existing `seed()` handler) |

**Secondary CTA.** An anchor `<a href="/docs/user-guide" target="_blank" rel="noopener noreferrer">` styled as `Btn kind="ghost"`. The user guide lives at `docs/user-guide/` in the repo; if not yet publicly hosted, point to the GitHub path.

**Behaviour.** The `EmptyState` atom already accepts `title`, `body`, and `action` props (see `frontend/src/components/ui/atoms.tsx:470`). The `action` slot can receive two siblings wrapped in a `<div style={{ display: 'flex', gap: 10 }}>`. No atom change needed.

**Zustand / store call.** None beyond what each page already does. `AppsPage` calls `appsApi.list()` and renders `EmptyServices` when `filtered.length === 0`; that local component is replaced by the `EmptyState` atom with the two-CTA variant. `PipelinesPage` and `EnvironmentsPage` already use `EmptyState`; each just gains the secondary anchor in the `action` slot.

**Existing atoms used.** `EmptyState`, `Card`, `Btn`. No new atoms.

---

## Task B — PR #38 bundle-split follow-up

Source files: `frontend/src/App.tsx` (main branch, post-merge), `frontend/vite.config.ts`.

### B1 — What PR #38 claimed

The PR commit message (`e94207b`) states:

> "React.lazy + Suspense around non-landing routes; Vite manualChunks splits react/xyflow/oidc/zustand. Entry chunk 490KB → 59KB (88% cut)."

The backlog entry (`backlog.md` closed section) itemises:
- `AppsPage`, `AppDetailPage`, `SignInPage`, `SignUpPage`, and `Callback` remain **eager** (fast first paint).
- All other 11 routes lazy-loaded.
- `manualChunks`: `react`, `xyflow`, `oidc`, `zustand`.

### B2 — What main actually has

Reading `frontend/src/App.tsx` on main (post-merge, lines 1–81):

**Eager imports (lines 10–14):** `AppsPage`, `AppDetailPage`, `SignInPage`, `SignUpPage`, `Callback`.

**Lazy imports (lines 19–29):** `NewAppWizard`, `PipelinesPage`, `PipelineEditorPage`, `RunPage`, `DockerPage`, `ComposePage`, `KubernetesPage`, `EnvironmentsPage`, `HostsPage`, `SettingsPage`, `RegistryPage`.

Reading `frontend/vite.config.ts` on main (lines 28–33): `manualChunks: { react, xyflow, oidc, zustand }`.

**Result: implementation matches the PR description exactly.** All 11 non-landing routes are lazy; the four vendor chunks are present; the xyflow chunk is genuinely isolated.

### B3 — Drift findings

**No factual drift** between the PR description and the current main implementation.

Three observations for the next planning round:

**1. `AppDetailPage` is eager but should be lazy.** The PR treats `AppDetailPage` as a "landing-page route" for first-paint. It is never the first page a new visitor lands on — it is always reached by clicking a row in `AppsPage`. Lazy-loading it would shrink the entry chunk further at no UX cost. **Recommended action: move `AppDetailPage` to the lazy group in a follow-up PR.**

**2. `AppDetailPage` opens a raw `new WebSocket(...)` (line 80).** This violates the CLAUDE.md hard rule ("don't open raw `new WebSocket(...)` in a component — use `useWebSocket`"). This is pre-existing hygiene debt surfaced during this scan, not introduced by PR #38. It belongs in the hygiene backlog alongside the lazy-load fix.

**3. `oidc-client-ts` is correctly in its own chunk (`oidc`)**, not merged into the React vendor chunk. PR #38 got this right; it landed correctly on main.

### B4 — Pages that should-but-don't-yet lazy-load

Only `AppDetailPage` qualifies (see B3 item 1). All other eager routes (`AppsPage`, `SignInPage`, `SignUpPage`, `Callback`) have a legitimate first-paint justification.

### B5 — Vendor chunks worth further splitting

No additional splitting is warranted. The four chunks cover the four large independent dependency trees. No single remaining dependency approaches `xyflow`'s ~150 KB size. `react-router-dom` is correctly grouped with `react`/`react-dom` (shared release cadence, always co-loaded).

---

## Summary

| Item | Source | Status |
|---|---|---|
| A1 — Webhook URL CTA | W11 §Indie step 5 | Wireframe complete; no backend change needed |
| A2 — Deployed URL "Visit" link | W11 §Indie step 6 | Wireframe complete; requires `url` on `AppDeployResponse` or `AppModel` |
| A3 — Empty-state CTAs (Apps / Pipelines / Envs) | W11 §Indie step 2 | Wireframe complete; `EmptyState` atom already sufficient |
| B — PR #38 drift | `App.tsx`, `vite.config.ts` | No drift. Most surprising: `AppDetailPage` is eager + uses raw `new WebSocket(...)` |
