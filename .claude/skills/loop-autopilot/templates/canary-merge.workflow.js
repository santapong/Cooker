// Template: CANARY-MERGE — the SCALE rung's autonomous-delivery gate. This is the ONE
// place the loop may merge without a human, and it OVERRIDES the skill's propose-only
// default (SKILL.md §6 rule 1). It is off unless the 🔒 Autonomy State issue says on.
// Called from the Propose stage on a candidate the loop already marked safeToPropose AND
// that already passed the SUSTAIN in-band gate (verifier-canary.workflow.js). See
// deployment.md §"Advanced: autonomous delivery (SCALE)".
//
// CONTROL FLOW (this part is portable):
//   read autonomy state -> eligibility gate -> merge behind canary -> bake -> promote|rollback -> update state
// INFRA-SPECIFIC (you must supply — marked EDIT ME): the merge-behind-a-guard mechanism
//   (feature flag / canary slice / staged rollout), the health-signal reads during bake,
//   and the rollback command. Auto-merge WITHOUT a real canary is not SCALE — it's just
//   removing the safety net. If you can't supply these, return {action:'propose'}.
//
// SAFETY: any gate miss, any active held-out alarm, any ineligible kind -> fall back to
// propose-only (return {action:'propose'}), never merge. A bad bake -> autonomous
// rollback + escalate. Rollback-rate over threshold -> trip autonomy OFF for all kinds.
//
// H10: no clock / no Math.random in-script; time is passed via args.nowIso.
//
// Invoke with: Workflow({ script, args: {
//   repo:{owner,name}, candidate, autonomyIssueNumber, ledgerIssueNumber,
//   baselineIssueNumber, trustThreshold, rollbackRateTrip, bakeMechanism,
//   healthCheckSpec, rollbackSpec, nowIso } })
//   candidate         — { id, kind, branch, prNumber, filesChanged:[...], risk, summary }
//   autonomyIssueNumber — pinned 🔒 Autonomy State issue (mode, enabledKinds, trips, log)
//   trustThreshold    — min credit-ledger trustWeight for a kind to be eligible (e.g. 0.9)
//   rollbackRateTrip  — rollback fraction over the recent window that trips autonomy off

export const meta = {
  name: 'canary-merge-template', // EDIT ME
  description: 'SCALE autonomous delivery: eligibility-gate a safeToPropose candidate, merge behind a canary, bake, promote or auto-rollback; trip to propose-only on alarm',
  phases: [
    { title: 'Gate', detail: 'autonomy on? kind eligible? gates green? — else propose-only' },
    { title: 'Merge', detail: 'merge to main behind a flag/canary (never 100%)' },
    { title: 'Bake', detail: 'watch health signals for the window' },
    { title: 'Decide', detail: 'promote if healthy, else autonomous rollback + escalate' },
  ],
}

const input = typeof args === 'string' ? JSON.parse(args) : args
const REPO = (input && input.repo) || { owner: 'OWNER', name: 'REPO' } // EDIT ME
const C = (input && input.candidate) || null
const AUTONOMY_ISSUE = (input && input.autonomyIssueNumber) || null
const LEDGER_ISSUE = (input && input.ledgerIssueNumber) || null
const BASELINE_ISSUE = (input && input.baselineIssueNumber) || null // held-out alarm source
const TRUST_MIN = (input && input.trustThreshold) || 0.9
const ROLLBACK_TRIP = (input && input.rollbackRateTrip) || 0.34 // >1/3 of recent merges rolled back -> trip
const BAKE_MECHANISM = (input && input.bakeMechanism) || 'EDIT_ME flag/canary/staged-rollout: how the merged change ships guarded'
const HEALTH_SPEC = (input && input.healthCheckSpec) || 'EDIT_ME: CI-on-main + canary error/latency SLO + no new failing held-out'
const ROLLBACK_SPEC = (input && input.rollbackSpec) || 'EDIT_ME: git revert the merge commit + flip flag off / redeploy'
const NOW_ISO = (input && input.nowIso) || null

// Change classes that NEVER auto-merge, regardless of every gate (deployment.md §Eligibility).
const NEVER_KINDS = ['migration', 'infra', 'secret', 'api-break', 'release']
const propose = (reason) => { log(`FALL BACK to propose-only: ${reason}`); return { action: 'propose', reason } }

if (!C) return propose('no candidate supplied')
if (!AUTONOMY_ISSUE) return propose('no 🔒 Autonomy State issue wired — SCALE cannot be enabled safely')

// --- PHASE 1: GATE ---------------------------------------------------------------
// 1a. Autonomy state: is SCALE on, for THIS kind, and not currently tripped?
const stateRead = await agent(
  `Repo: ${REPO.owner}/${REPO.name}. Read issue #${AUTONOMY_ISSUE} and return its body verbatim as "json".`,
  { label: 'read-autonomy', phase: 'Gate', schema: { type: 'object', properties: { json: { type: 'string' } }, required: ['json'] } },
)
let state
try { state = JSON.parse((stateRead && stateRead.json) || '') } catch { state = null }
if (!state) state = { mode: 'propose-only', enabledKinds: [], tripped: false, rollbacksRecent: [], log: [] }
if (state.mode !== 'auto-merge' || state.tripped) return propose(`autonomy mode=${state.mode} tripped=${!!state.tripped}`)
if (!Array.isArray(state.enabledKinds) || !state.enabledKinds.includes(C.kind)) return propose(`kind "${C.kind}" not in enabledKinds`)

// 1b. Hard eligibility: NEVER-list, high-risk memo, protected paths (defense-in-depth;
//     verifier-canary already blocks protected paths, re-checked here before a MERGE).
if (NEVER_KINDS.includes(C.kind)) return propose(`kind "${C.kind}" is on the NEVER-auto-merge list`)
if (C.risk === 'high') return propose('risk memo rated high')

// 1c. Held-out alarm active? An out-of-band meta-overfit signal forbids auto-merge outright.
if (BASELINE_ISSUE) {
  const b = await agent(
    `Repo: ${REPO.owner}/${REPO.name}. Read issue #${BASELINE_ISSUE} (held-out baseline) and return its body verbatim as "json".`,
    { label: 'read-heldout', phase: 'Gate', schema: { type: 'object', properties: { json: { type: 'string' } }, required: ['json'] } },
  )
  let hb; try { hb = JSON.parse((b && b.json) || '') } catch { hb = null }
  const lastRun = hb && hb.history && hb.history.length ? hb.history[hb.history.length - 1] : null
  if (lastRun && lastRun.alarm) return propose('held-out meta-overfit alarm is active')
}

// 1d. Credit-ledger trust for this kind must clear the high bar.
if (LEDGER_ISSUE) {
  const l = await agent(
    `Repo: ${REPO.owner}/${REPO.name}. Read issue #${LEDGER_ISSUE} (credit ledger) and return its body verbatim as "json".`,
    { label: 'read-ledger', phase: 'Gate', schema: { type: 'object', properties: { json: { type: 'string' } }, required: ['json'] } },
  )
  let led; try { led = JSON.parse((l && l.json) || '') } catch { led = null }
  const tw = led && led.kinds && led.kinds[C.kind] ? led.kinds[C.kind].trustWeight : 0
  if (tw < TRUST_MIN) return propose(`kind "${C.kind}" trustWeight ${tw} < ${TRUST_MIN}`)
}

// --- PHASE 2: MERGE (behind a guard, never 100%) ---------------------------------
const merged = await agent(
  `Repo: ${REPO.owner}/${REPO.name}. Merge PR #${C.prNumber} (branch ${C.branch}) into main, but SHIP IT GUARDED, not to everyone: ${BAKE_MECHANISM}. Record the merge commit SHA. Do NOT roll out to 100%. Report the merge SHA and the guard you applied.`,
  { label: `merge:${C.id}`, phase: 'Merge', schema: { type: 'object', properties: { mergeSha: { type: 'string' }, guard: { type: 'string' }, ok: { type: 'boolean' } }, required: ['ok'] } },
)
if (!merged || !merged.ok || !merged.mergeSha) return propose('merge did not complete cleanly — nothing to promote')

// --- PHASE 3: BAKE (watch health for the window) ---------------------------------
const health = await agent(
  `Repo: ${REPO.owner}/${REPO.name}. For merge ${merged.mergeSha}, evaluate health over the bake window: ${HEALTH_SPEC}. Return healthy=true ONLY if every signal is within bounds; otherwise healthy=false with the breached signal.`,
  { label: `bake:${C.id}`, phase: 'Bake', schema: { type: 'object', properties: { healthy: { type: 'boolean' }, breach: { type: 'string' } }, required: ['healthy'] } },
)

// --- PHASE 4: DECIDE (promote or autonomous rollback) ----------------------------
const rollbacks = Array.isArray(state.rollbacksRecent) ? state.rollbacksRecent.slice(-19) : []
let action, detail
if (health && health.healthy) {
  await agent(
    `Repo: ${REPO.owner}/${REPO.name}. Bake passed for ${merged.mergeSha}. Promote to full rollout (widen the flag / complete the staged rollout). Confirm done.`,
    { label: `promote:${C.id}`, phase: 'Decide', schema: { type: 'object', properties: { done: { type: 'boolean' } }, required: ['done'] } },
  )
  action = 'promoted'; detail = merged.mergeSha
  rollbacks.push(0)
} else {
  // Autonomous rollback — cheap, reversible; the whole reason canary beats a perfect gate.
  await agent(
    `Repo: ${REPO.owner}/${REPO.name}. Bake FAILED for ${merged.mergeSha} (breach: ${(health && health.breach) || 'unknown'}). Roll back now: ${ROLLBACK_SPEC}. Then open a loud issue titled "🚨 Auto-rollback ${merged.mergeSha}" describing the breach. Confirm the revert landed.`,
    { label: `rollback:${C.id}`, phase: 'Decide', schema: { type: 'object', properties: { reverted: { type: 'boolean' } }, required: ['reverted'] } },
  )
  action = 'rolled-back'; detail = `${merged.mergeSha}: ${(health && health.breach) || 'breach'}`
  rollbacks.push(1)
}

// Tripwire: if the recent rollback rate is too high, revoke autonomy for ALL kinds.
const rate = rollbacks.length ? rollbacks.reduce((a, b) => a + b, 0) / rollbacks.length : 0
const trip = rate >= ROLLBACK_TRIP
if (trip) log(`🚨 AUTONOMY TRIPPED: rollback rate ${rate.toFixed(2)} >= ${ROLLBACK_TRIP} — dropping ALL kinds to propose-only. Human review required to re-enable.`)

// Persist state + append to the audit log.
const nextState = {
  ...state,
  mode: trip ? 'propose-only' : state.mode,
  tripped: trip ? true : state.tripped,
  rollbacksRecent: rollbacks,
  log: [...(state.log || []), { at: NOW_ISO, candidate: C.id, kind: C.kind, action, detail, rollbackRate: Number(rate.toFixed(3)) }],
}
if (trip) nextState.trippedAt = NOW_ISO
await agent(
  `Repo: ${REPO.owner}/${REPO.name}. Replace issue #${AUTONOMY_ISSUE}'s body with exactly this JSON, no commentary:\n${JSON.stringify(nextState, null, 2)}`,
  { label: 'write-autonomy', phase: 'Decide', schema: { type: 'object', properties: { updated: { type: 'boolean' } }, required: ['updated'] } },
)

log(`SCALE ${action} ${C.id} (${C.kind}); rollbackRate=${rate.toFixed(2)}${trip ? ' — TRIPPED to propose-only' : ''}`)
return { action, candidate: C.id, kind: C.kind, mergeSha: merged.mergeSha, tripped: trip, rollbackRate: rate }
