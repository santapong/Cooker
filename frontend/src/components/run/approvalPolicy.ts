import type { Environment } from '../../types/environment';
import type { EnvironmentStatus, StageApproval } from '../../types/pipeline';

export type GateLoadState = 'loading' | 'ready' | 'unavailable';

/** Independent server permissions: operators invoke, dedicated approvers vote. */
export function runRolePolicy(roles?: readonly string[]) {
  return {
    canInvoke: !!roles?.some((role) => role === 'admin' || role === 'operator'),
    canApprove: !!roles?.some((role) => role === 'admin' || role === 'approver'),
  };
}

const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const text = (value: unknown): value is string => typeof value === 'string' && value.length > 0;

/** Reject foreign/malformed snapshots instead of turning failed reads into empty gates. */
export function readGateSnapshot(value: unknown, runId: string): StageApproval[] | null {
  if (!record(value) || value.runId !== runId || !Array.isArray(value.gates)) return null;
  const stages = new Set<string>();
  for (const gate of value.gates) {
    if (!record(gate) || !text(gate.id) || gate.runId !== runId || !text(gate.stageId) || stages.has(gate.stageId) || typeof gate.status !== 'string' || !['awaiting', 'approved', 'rejected'].includes(gate.status) || !Number.isSafeInteger(gate.requiredApprovers) || Number(gate.requiredApprovers) < 1) return null;
    if (gate.votes !== undefined && (!Array.isArray(gate.votes) || gate.votes.some((vote) => !record(vote) || typeof vote.approverEmail !== 'string' || typeof vote.createdAt !== 'string' || vote.note !== undefined && typeof vote.note !== 'string'))) return null;
    if (gate.resolvedBy !== undefined && typeof gate.resolvedBy !== 'string' || gate.resolvedAt !== undefined && typeof gate.resolvedAt !== 'string') return null;
    stages.add(gate.stageId);
  }
  return value.gates as StageApproval[];
}

export function gateDecisionMessage(status: string, kind: 'approve' | 'reject'): string | null {
  if (kind === 'approve' && status === 'awaiting') return 'Approval recorded. This gate still needs more approvals.';
  if (kind === 'approve' && status === 'approved') return 'Approval gate approved.';
  if (kind === 'reject' && status === 'rejected') return 'Approval gate rejected.';
  return null;
}

export function promotionDecisionMessage(status: string, kind: 'approve' | 'promote'): string | null {
  if (kind === 'approve' && status === 'awaiting_approval') return 'Promotion approval recorded. More approvals are still required.';
  if (kind === 'approve' && status === 'approved') return 'Promotion approved. Deployment completion is reported separately.';
  if (kind === 'promote' && ['pending', 'awaiting_approval', 'approved', 'deploying', 'deployed'].includes(status)) return 'Promotion recorded. Check its status for progress; approval does not confirm deployment completion.';
  return null;
}


/** A success status alone cannot confirm a different persisted run/gate. */
export function confirmedGateDecision(value: unknown, expected: StageApproval, kind: 'approve' | 'reject'): string | null {
  if (!record(value) || value.runId !== expected.runId || value.stageId !== expected.stageId) return null;
  const gates = readGateSnapshot({ runId: expected.runId, gates: [value.gate] }, expected.runId);
  const next = gates?.[0];
  if (!next || next.id !== expected.id || next.requiredApprovers !== expected.requiredApprovers || next.status !== value.status) return null;
  const count = next.votes?.length ?? 0;
  if (kind === 'approve' && (!Number.isSafeInteger(value.approvalsHave) || value.approvalsHave !== count || value.approvalsNeed !== next.requiredApprovers || next.status === 'approved' && count < next.requiredApprovers || next.status === 'awaiting' && count >= next.requiredApprovers)) return null;
  return gateDecisionMessage(next.status, kind);
}

/** Read exact current-run promotion evidence without legacy empty-value coercion. */
export function readPromotionSnapshot(environments: unknown, value: unknown, runId: string): { environments: Environment[]; statuses: EnvironmentStatus[] } | null {
  if (!Array.isArray(environments) || !record(value) || value.runId !== runId || !Array.isArray(value.statuses)) return null;
  const ids = new Set<string>();
  for (const env of environments) {
    if (!record(env) || !text(env.id) || !text(env.name) || !Number.isSafeInteger(env.order) || ids.has(env.id) || !record(env.promotion) || typeof env.promotion.strategy !== 'string' || !['auto', 'manual'].includes(env.promotion.strategy)) return null;
    if (env.promotion.requiredApprovers !== undefined && (!Number.isSafeInteger(env.promotion.requiredApprovers) || Number(env.promotion.requiredApprovers) < 1)) return null;
    ids.add(env.id);
  }
  const seen = new Set<string>();
  for (const status of value.statuses) {
    if (!record(status) || !text(status.environmentId) || !ids.has(status.environmentId) || seen.has(status.environmentId) || typeof status.status !== 'string' || !['pending', 'deploying', 'deployed', 'failed', 'awaiting_approval', 'approved'].includes(status.status)) return null;
    if (status.approvalsHave !== undefined && (!Number.isSafeInteger(status.approvalsHave) || Number(status.approvalsHave) < 0) || status.approvalsNeed !== undefined && (!Number.isSafeInteger(status.approvalsNeed) || Number(status.approvalsNeed) < 1) || status.promotedAt !== undefined && typeof status.promotedAt !== 'string' || status.approvedBy !== undefined && typeof status.approvedBy !== 'string') return null;
    seen.add(status.environmentId);
  }
  return { environments: environments as Environment[], statuses: value.statuses as EnvironmentStatus[] };
}
