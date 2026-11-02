# Cooker — current status and remaining tasks

Reconciled 3 October 2026 against the local candidates, retained runtime, dated
implementation records and the 2 October Vercel audit. The original Cooker checkout is
clean `develop` at `949d4a9`. Implementation remains in Cooker-backend and Cooker-landing,
with pending work preserved. No fresh remote fetch or release is implied.

Reconciled after the 16:21 UTC combined activation: localhost4644 runs the checked
coverage, prospective webhook audit036, notification035/status UI and immutable
secret-version build. Activation, rollback to retained R0 and reactivation all
passed JSON/database readiness while preserving resource counts and existing
auth/encryption configuration. Later polling observed 13 accessible and 13
selected projects in the connected team with zero sync failures. Active authored
DAG source and registry-authority edits are excluded. Use the
[full roadmap](../plans/2026-10-02-consolidated-feature-roadmap.md) and
`/mnt/data/cooker-roadmap-20261002/progress.json` for current implementation scope.

## Current product

Cooker targets self-hosted users and small teams. The first outcome remains
dependable Git-to-deploy automation. Vercel is the first provider integration:
connect an existing account and understand its deployed applications before adding
broader provider control.

| Area | Current implementation | Verification boundary |
| --- | --- | --- |
| Branding and frontend | Ready Block mark beside the existing name; shared favicon; charcoal/ember and Paper; landing, real sign-in/workspace and task documentation | Local builds and scoped browser checks. Owner selected Ready Block; public landing update remains pending |
| GitHub / Google sign-in | Explicit Keycloak broker configuration, provider buttons on sign-in/signup, allowlisted provider hints, safe return navigation and setup guide | Implemented locally; 271 frontend tests, affected Go packages, build and browser checks pass. OAuth registrations and broker activation are pending; no live provider login yet |
| Native deployment | Reviewed source, durable App/pipeline jobs, immutable images, BuildKit/Crane handoff, recovery, service permissions, masked stage logs and exact-container Docker logs | Real isolated Redis/n8n/Gitea/registry/worker-recovery and same-version backup checks recorded 28 September; hosted production acceptance remains separate |
| Git automation | GitHub/Gitea/GitLab connections, authenticated webhook admission, branch/tag rules, durable decisions and review | Real local private Gitea webhook-to-Docker verified; hosted GitHub and real GitLab acceptance pending |
| Vercel connection | Encrypted token, account/team/project selection, read-only polling, history, pause/replacement/disconnect | Existing owner connection works; 2 October audit found 13/13 selected team projects, no missing or duplicate apps. This is scoped to that connected team |
| Unified workspace | Apps, global/app Services, read-only Vercel flows in Pipelines, history, freshness and provider links; exact-production aliases and Application aggregate explanation | 14:05 UTC read-only audit matched 13 selected apps/flows and all 13 production alias sets. Detailed ordinary-service proof and signed-in owner navigation remain open |
| Public landing | Existing Cooker landing is hosted on its original Vercel project | Previous seven-file publication verified; newest branding has not been republished |

Ready Block's short assembly motion is decoration, not a deployment-readiness
signal. Favicons and workspace branding remain static. The login mark assembles
once; reduced-motion CSS leaves it completed and still. Existing auth and actions
remain intact.

## Ordered remaining work

The requested social-login slice is prepared. Follow
[GitHub and Google setup](../user-guide/operations/social-sign-in.md) to register
the apps, configure the broker and rebuild with OIDC enabled. Current localhost
4644 intentionally retains local authentication and shows Setup needed. This is
separate from Vercel OAuth onboarding and does not change the remaining catalog
or deployment acceptance gates. Evidence and rollback:
`/mnt/data/cooker-social-auth-20261002/README.md`.

### 1. Complete accurate Vercel catalog acceptance

**Why first:** users need accurate answers about components and production
destinations before more provider controls are added.

- Ordinary applications: the newer build explains eleven owner-observed Application
  deployment aggregates while leaving detailed inventory unknown. Missing service
  metadata does not prove a single-service application. Strict ordinary-build
  classification remains open.
- Aliases: exact current-production alias observations and safe typed persistence
  are implemented. The later 14:05 UTC audit matched all 13 alias sets, including
  the two resume-blog aliases missing in the initial audit. Keep deployment URLs,
  alias freshness and partial failures distinct.
- Coverage: scoped discovery/selection counts and searchable pages passed local
  checks and were rolled out. Verify the actual owner's selection
  and navigation. Do not automatically select newly discovered projects.

Initial findings: `/mnt/data/cooker-catalog-audit-20261002/README.md`.
Later scoped evidence: `/mnt/data/cooker-roadmap-20261002/read-only-audit/`,
`cat02/` and `cat05/`. This does not establish inventory across other accounts/teams.

### 2. Complete signed-in local acceptance

**Why next:** fixture success and correct inventory do not prove the owner's
day-to-day experience.

- Owner signs in to the existing account at http://127.0.0.1:4644/signin.
- Follow landing App → Services → Vercel flow → exact deployment history → provider
  dashboard; compare the existing publication/project/deployment identities.
- Check production versus preview, freshness/polling, unavailable states, both
  themes, keyboard navigation and phone layouts. Verify supported native editing
  and role rules separately from read-only imported records.
- Keep readiness distinct from application health. Leave credentials in the secure
  form; no new account or token is needed for ordinary navigation acceptance.

This human sign-in gate remains open. Do not bypass it or certify it from fixtures.

### 3. Finish dependable Git-to-deploy release acceptance

**Why before more ecosystems:** the product's first promise needs one complete
hosted source-to-runtime proof.

- Hosted disposable GitHub: signed delivery → exact private branch/tag source →
  reviewed App APIs → durable queue → isolated production build → digest artifact
  → real runtime/readiness/logs. Requires a chosen repository/connection and scoped
  acceptance authorization.
- Real GitLab: repeat source/webhook/ref/private-credential/revocation/recovery
  acceptance. Gitea's local proof does not certify GitLab.
- Extend worker-loss/cancellation testing beyond the proven post-readiness case;
  test supported uncertain provider outcomes and older-schema upgrade restore.
  Same-version backup evidence is not an upgrade guarantee.
- Accept the locally rolled out immutable external-secret versions, prospective
  delivery auditing and durable notification status. Atomic scheduler occurrence
  admission is also active. Complete actual process-loss/recovery and upgrade
  acceptance; the quiescent application rollback is a separate, narrower proof.
- Finish per-App/Pipeline registry authority, secret-editing/key-rotation behavior
  and measured queue/acknowledgment performance. Preserve fail-closed log replay.
  Authored Git-triggered DAGs still need verified worker source checkout; commit
  metadata on an event is not evidence that a build used that source.

Details: [implementation and release gates](../plans/2026-09-28-backend-implementation.md).

### 4. Publish the updated landing to the existing project

**Why after R1 reliability and review:** preserve the existing address and deployment history while
publishing the selected, verified branding.

- Current publisher creates a new dedicated project. It cannot replace the current
  landing; invoking it again would not perform the intended update.
- Prepare a reviewed existing-project update path tied to the retained provider
  account, project ID and exact public seven-file bundle. Preserve durable intent,
  ambiguous-write recovery and retained provider IDs; do not replay uncertain POSTs.
- Publish only under an explicit existing-project publication instruction, then
  compare anonymous served bytes, icon, allowed paths and production address.
  Keep backend/auth/workspace/prototype files excluded.

Current address: https://cooker-landing-cbf38579.vercel.app/.
This task did not publish it or change domains, credentials or hosted projects.

### 5. Add provider and deployment controls in bounded slices

- Vercel OAuth onboarding requires a registered integration/app and validated
  account/team ownership. A token form must remain labelled a token connection.
- Vercel deploy/promote/rollback and provider logs require explicit ownership,
  immutable source/artifacts, permission checks and durable reconciliation.
- Native promotion dispatch, rollback/canary orchestration, authored-pipeline Git
  triggers and source-token rotation controls remain unfinished. Delivery-decision
  browsing and prospective authenticated-attempt history are locally rolled out;
  actual owner acceptance remains open.
- Runtime replica/time-range/search/download controls and immutable Kubernetes
  pod log sessions remain follow-up work; current Docker evidence does not cover
  Kubernetes/ECS/Cloud Run logs.

### 6. Domains, tunnels and AWS/GCP expansion

Users may use an operator-supplied base domain or a supported provider address;
they do not always need to own a domain. Cooker still needs DNS/TLS/tunnel
infrastructure supplied by an operator/provider.

Domain ownership verification, DNS instructions, certificate renewal/lifecycle,
stable tunnel management, AWS/GCP health/logs/URLs/recovery and live-account
acceptance remain future releases. Existing configured targets are not complete
ecosystem integrations. Use an agreed provider/resource scope for each campaign.

### 7. Integrate and release the candidate

After the above first-release gates are agreed and satisfied, package coherent
reviewable changes, run required final checks and integrate through the repository's
develop workflow. The current candidate is uncommitted/unreleased; no merge, push,
tag or public Cooker backend deployment occurred in this task.

## Sources and limits

- Branding integration/build/browser/runtime evidence:
  `/mnt/data/cooker-brand-integration-20261002/README.md`.
- Selected icon: `/mnt/data/cooker-ready-block-20261002/README.md`.
- Unified catalog: [contracts and local rollout](unified-catalog.md),
  `/mnt/data/cooker-catalog-20261002/README.md`.
- Existing provider/deployment work: [capability guide](README.md),
  [implementation record](../plans/2026-09-28-backend-implementation.md),
  [Vercel slices](vercel-sync.md#next-roadmap-slices).

Older dated notes saying the account is unconnected or Vercel inventory has never
been compared are historical. The connected-account audit supersedes those claims
within its scope; it does not close owner sign-in, catalog detail, future provider
controls or release gates. These priorities are a proposed execution order for
the remaining roadmap, not a claim that all future features were implemented.


## Coherent source and registry candidate — 3 October ICT

The isolated 807-file backend passed 2,466 default race cases, both vet profiles
and a static build. The preceding 806-file freeze passed actual PostgreSQL
registry/App 58, native 31 and lifecycle 77 scoped cases; the two later files guard retained-canary
promotion and do not execute in those PG profiles. Original negative/harness
failures remain in the [coherent evidence](/mnt/data/cooker-roadmap-20261002/coherent-source-registry/README.md).

The source/registry UI baseline passed 299 unit cases and 18 owned Chromium cases.
Its native Git rule/history extension passed 311 unit cases and 38 owned Chromium
cases, with both auth-profile builds and scoped lint. These fixtures do not admit
a real webhook or deployment. The separate focused stage-approval UI repair
passed44 policy tests,24 Chromium cases, zero-warning ten-file lint and both
TS/build profiles. It covers role/quorum/freshness/response handling and retains
four reproduced failures. Prior38 browser cases were not rerun. Full RunView HUD
polish remains open at390px. See the
[approval UI handoff](/mnt/data/cooker-roadmap-20261002/stage-approval-ui/frozen-v3/README.md).

Subsequent capability metadata now respects scoped Docker/Compose transport;
343 affected handler/server race cases and vet passed. A reproduced stage-approval
race now preserves the first terminal decision, serializes votes and reports late
conflicts honestly. Its final handler/server freeze passed 348 race cases and both
vet checks. The unchanged service/store/jobqueue inputs retain 943 successful
cases with seven optional skips. One actual PostgreSQL fixture checked 16
concurrent votes and 16 competing terminal decisions; owned resources were cleaned.
The initial raw-error helper bypass failure is retained, and the repaired handler
returns fixed summaries for stage-store failures. The earlier run-store lookup
is still an open raw-error path; independent review also identified the final-vote
commit-to-resolution interruption window. These source-reviewed findings need
new executable reproductions and repairs, as recorded in the
[backend approval review](/mnt/data/cooker-roadmap-20261002/coherent-source-registry/stage-approval/review-20261003/README.md). Exact prior check scopes are in the
[stage approval record](/mnt/data/cooker-roadmap-20261002/coherent-source-registry/stage-approval/after-v2/verification.json).

These later deltas are outside the earlier full-backend freeze. They do not prove
whole-process loss, upgrade, hosted-provider or signed-in owner acceptance. The
owner's running localhost4644 build is unchanged and excludes this slice. Full
GIT-03/SEC-04 and roadmap release gates remain open.


## Latest approval and manual App acceptance — 3 October ICT

Atomic final-vote resolution, legacy quorum recovery without invented identity,
safe approval run lookup and persisted rejection/recovery responses passed1,320
affected race cases, both vet profiles and static build.32 actual owned
PostgreSQL cases passed;76 memory cases passed on a later test-only overlay.
[Exact approval evidence](/mnt/data/cooker-roadmap-20261002/coherent-source-registry/stage-approval/atomic-resolution-20261003/README.md) retains all failures and scope boundaries.

The genuine manual App fixture now passes source checkout, BuildKit OCI,
authenticated publication/pull, Docker health, RuntimeService observation and
generation-bound redacted logs. Log rotation denies a new historic session;
three receipt retries add no effects. The temporary checkout path found in a
persisted graph is scrubbed only on its cloned ReviewOnly view.
[Actual App acceptance](/mnt/data/cooker-roadmap-20261002/registry-authority/app-runtime/trial-bbae9ee4-c185-4842-a9a3-425efea14f54/acceptance.json) is an owned scratch/manual fixture, not hosted Git, automatic Dockerfile App
triggers, private base pulls or complete worker restart. All trial resources
were checked absent after exact ownership cleanup. Owner localhost4644 remains
the earlier build and passed JSON/db readiness at20:58UTC. Full release gates
remain open.


## Private base and fresh-worker scoped acceptance — 3 October ICT

Three real private-FROM cases and two new-process first-claim revocation cases
passed on separate final freezes. Basic BuildKit Credentials/base-layer/runtime
identity was verified on an owned HTTP authority; revocation stopped both native
and App work before14counted effects. App queue completion means its failed
preflight was recorded, but AppDeploy history rows0 remain a product gap. Atomic
failed-history repair is in progress.146private-base scoped races,826service
races and both vets passed; opt-in skips/retained failures/cleanup are explicit.
Exact evidence: /mnt/data/cooker-roadmap-20261002/registry-authority/private-base/README.md
and registry-authority/fresh-worker-revocation/HANDOFF.md under that root.

The combined817-file freeze passed integration vet/static build with unchanged
production binary bytes. Separate actual proofs are not a combined runtime or
full-suite rerun. Root reproduced390px header overlap;16layout/24approval
fixtures, both build profiles and scoped lint passed on the repaired frontend.
Full graph/console/App/owner responsive acceptance remains open. Evidence is
registry-authority/r1-runtime-combined-20261003/verification.json and
run-hud-layout/after-v1/verification.json under the same root. Owner4644image
7777698f... is unchanged, JSON/db readiness21:28:37UTC. No owner activation.
Whole process-loss/upgrade, hosted-source/owner walkthroughs, broader registry
transports/base policy and full45-package release acceptance remain unfinished.


## App rejection history and runtime observation — 3 October 2026 ICT

The original App history rows0 gap is historical. The final bounded registry
preflight repair atomically records FAILED run + one typed rejected attempt,
checks actual immutable claim/payload authority, and preserves the exact business
failure after lost queue acknowledgement. Malformed modern/legacy envelopes and
missing callback observation capabilities fail closed. Native runtime/log reads
exclude only rejected attempts, preserving ordinary failed/uncertain history.
App/history read failures settle independently with fixed summaries, labelled
last-known data and late-response fencing.

The reviewed832-file v7 backend passed1,386affected race entries,248opt-in skips,
scoped vet/static build. Four actual owned PG profiles passed65entries on v6;
v7's memory callback/test delta has exact unchanged SQL/producer/fixture parity,
not a whole-v7PG rerun. Final frontend-v4 passed7explicit-theme fixtures, both
builds and zero-warning lint after retained negatives. All owned labels/children
were checked absent. Source/build/review/scopes:
/mnt/data/cooker-roadmap-20261002/registry-authority/app-preflight-combined-20261003/verification.json.

Owner4644image7777698f... remains unchanged, final JSON/db readiness22:35UTC.
No activation/provider/account/credential/key change. Older builds parse the new
kind but lack runtime exclusion; typed-history downgrade behavior must be tested
before rollout without erasing failed history. Next: after-runtime worker loss,
other preflight reasons, upgrade/downgrade, remaining transports/base policy and
hosted/owner acceptance. All45packages/20gates retain not_proven completion;
12invariant definitions remain. Earlier pending atomic-history entries are
historical, not unresolved registry-preflight gaps.


## Typed-history rollback readers — 3 October 2026 ICT

The typed-history read compatibility item is locally verified, superseding only
that earlier pending read item. The actual retained owner727source plus the shared
test fails all6unchanged PostgreSQL scenarios: latest rejected history hides
recorded services and selects the rejected configuration for log lookup. An
isolated owner-based729-file source with5production read backports and current
candidate833(832basis+same test) pass28actual-PG read entries over the same stored
rows, including owned container/PostgreSQL restart. All7fixture tables including
schema migrations retain identical hashes. Compatibility selected unit checks
pass28entries/21top-level/5packages with one unconfigured PG unit skip; scoped
integration vet/static production build pass. No complete backend/UI suite rerun.
Exact sources, binaries, original negative, failed fixture setups, final cleanup
and independent bounded review are recorded at:
/mnt/data/cooker-roadmap-20261002/registry-authority/typed-history-rollback/README.md
and verification.json. All owned containers/networks/volume were checked absent.

This does not prove old-worker/modern-job execution, in-flight upgrade, schema
down migrations, owner backup/key restoration or executable rollback. The old
execution/admission code remains unchanged in the compatibility artifact, so
that binary must not replace the owner by inference. Owner4644is still image
7777698f..., JSON/db readiness rechecked23:03:53UTC2October. No owner activation,
provider or credential change. Full45packages/20gates retain not_proven;
12invariant definitions remain. Native/App after-runtime campaigns are separately
in progress; the initial AppSIGKILL boundary passed but final stronger handoff
is pending. Native first-claim heartbeat/reconstruction race is being reproduced
before any production repair. These in-progress observations are not completion.

The repeated PostgreSQL restart trap and cross-version semantic read procedure
were saved in cooker-maintenance/references/backend-acceptance.md with local
backup/evidence under typed-history-rollback/learning. Structural quick_validate
passed; future reuse benefit remains unmeasured.

## Named source-worker recovery and checkout receipts — 3 October 2026 ICT

Two actual SIGKILL boundaries passed on owned local resources: native after real
Docker readiness but before deployment result returns, and manual App after
terminal run/history persistence but before queue completion. Different fresh
recovery OS processes retain attention and immutable source/receipt/stage/runtime
evidence without execution replay. Stale writes and transaction resolution faults
are fenced; independent runtime inspection precedes explicit resolution. The
second job is claimed only for key-release proof, then disposed of without
dispatch. Exact labelled cleanup records are empty.

The native campaign reproduced and repaired the immediate-heartbeat/missing-run
race and nondurable checkout observation. A bounded missing-view predicate adds
no execution authority; the new claim-fenced source-only recorder commits before
stages and preserves concurrent progress/heartbeat. Missing capability/storage
errors stop stages with fixed public text, retained cancellation cause and private
source cleanup. Actual PostgreSQL recorder profile passed22entries/4top/0skips,
covering claim/status/lineage/timestamp/concurrency/skip/rewrite/commit/lock faults.
No Run.StartedAt was invented to fill unavailable generic lifecycle evidence.

Final combined843-file backend exactly matches current candidate; manifest
c6603b3a21f871a0fc4a19403e9b7de5f1a07200637b8a3943c623c4088a9eac.
Affected1501race entries/784top/8existing optional skips/8packages, default and
integration scoped vet, static build pass. Binarya889524430f3b517eed5c512f16efc41d47e85d919eb0352a2c5d13e60024ce3.
Native839/App833 runtime freezes remain separately scoped; no whole-backend/UI
or combined runtime rerun. Root verified all source and peer handoff hashes.
Initial combined PG compile and missing-unit-claim negatives remain preserved;
corrected production and actual-claim fixture passed final checks. Independent
six-file review reports no remaining bounded finding.

Evidence and reproducible inputs:
/mnt/data/cooker-roadmap-20261002/registry-authority/process-loss-combined/README.md
and verification.json. Owner4644image7777698f... unchanged; JSON/db readiness
passed23:37:47UTC2October. No owner activation, provider/account/credential/key
change, public deployment, commit/merge/push. Full45packages/20gates retain
not_proven;12invariant definitions remain. Next: executable modern-job
upgrade/rollback, other interruption/cancellation windows, in-flight revocation,
remaining transports, hosted/owner journey and coherent local release integration.
Earlier in-progress native/App summaries are historical for these named cases.

The focused recovery/checkout procedure is saved in cooker-maintenance/references/
source-worker-recovery.md and linked from SKILL.md/backend-acceptance.md, with
backups under process-loss-combined/learning. Structural quick_validate passed;
future reuse benefit remains unmeasured. Reader compatibility stays separate
from executable old-worker safety or full release acceptance.

## Executable compatibility and schema authority — 3 October 2026 UTC

Original retained727 workers are incompatible with modern jobs. Native actualPG
admissions for automatic/review each reach guarded Build/Push/DockerDeploy once
after typed decoding discards source/trigger/registry. App real-memory admission
and claim reach guarded checkout after grant revocation. No actual checkout,
build, push, pull or deploy runs in these probes. Current native missing-preparer
control proves only refusal, not configured source execution. Original727 and
reader729 remain unsupported execution fallbacks.

Fresh identical old/current SQL lifecycle tests validate a complete App rejection
marker before actual claim/Suspend. Old overwrites valid FAILED with attention and
clears FinishedAt; prospective843 preserves exact failed run/history while queue
becomes attention. Two current cases pass without skips; ordinary artifact and
stale-write controls pass on both. Initial importer replaced CreatedAt and made
its marker invalid; those limited originals are retained and superseded only by
the corrected twins. This is not actual currentPG App admission or full fallback.

Migration038 down/up reproduced grant/pin erasure and generation6→1. Local guard
rejects referenced authority under complete job→registry locked transaction and
retains sequence for ungranted legacy reapplication. Actual admitted ASCII-case
and long-s variants exposed an initial exact-key gap; final locale-independent
all-key guard passes7actualPG entries/0skips, including existing legacy control.
Independent source review found no scoped blocker. No owner schema reversal.

Final846 backend exactly matches candidate; manifest 5cd6d10d98426b2aa5ed462b4e1b0f7c2b288ebb9b7778645e4446c8ac73b584.
Only2production SQL files and3owned tests differ from prior843. Store192race/84top/
3packages/8optional skips; later full default2631race/1503top/64packages/21optional
skips, both full vets, formatting and production build pass. Binary
fa11eaeaae0aab56f5a648ad89175c25ac28085272f5838dcf6c406481ab44f6.
Actual integration remains named profiles; frontend not rerun. Root independently
verified peer source/artifact hashes, correction provenance and exact cleanup.

Evidence: /mnt/data/cooker-roadmap-20261002/registry-authority/executable-compatibility-combined/README.md
Owner4644image7777698f... unchanged, JSON/db readiness00:44UTC3October. No
activation/provider/account/credential/key/publication/commit/merge/push. Full45
packages/20gates remain not_proven;12invariant definitions retained. Next: accept
feature-compatible843 fallback startup, retained work and re-upgrade; remaining
crash/cancellation/revocation/transport/hosted/owner/coherent release requirements.
A proposed fallback is not accepted merely because its named lifecycle passes.

Reusable decoder/lifecycle/marker-import/schema-fold lessons saved in
cooker-maintenance/references/source-worker-recovery.md with backup under
executable-compatibility-combined/handoff-before. Structural quick_validate passed;
future reuse benefit unmeasured.
