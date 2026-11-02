import type { StageApproval } from '../../types/pipeline';
import { describe, expect, it } from 'vitest';
import { confirmedGateDecision, gateDecisionMessage, promotionDecisionMessage, readGateSnapshot, readPromotionSnapshot, runRolePolicy } from './approvalPolicy';

const gate = { id: 'gate', runId: 'run', stageId: 'stage', status: 'awaiting', requiredApprovers: 2, votes: [{ approverEmail: 'owned@fixture.invalid', createdAt: '2026-10-02T00:00:00Z', note: 'Reviewed' }] };

describe('approval permissions and retained gate evidence', () => {
  it.each([
    [['viewer'], false, false], [['operator'], true, false], [['approver'], false, true], [['admin'], true, true], [['viewer', 'approver'], false, true], [['operator', 'approver'], true, true], [[], false, false], [['unknown'], false, false],
  ] as const)('keeps invoke and approval authority distinct for %j', (roles, canInvoke, canApprove) => {
    expect(runRolePolicy(roles)).toEqual({ canInvoke, canApprove });
  });
  it('accepts explicit empty evidence and original run gates without modifying votes', () => {
    expect(readGateSnapshot({ runId: 'run', gates: [] }, 'run')).toEqual([]);
    const input = structuredClone(gate); expect(readGateSnapshot({ runId: 'run', gates: [input] }, 'run')).toEqual([gate]); expect(input).toEqual(gate);
  });
  it.each([
    null, {}, { runId: 'other', gates: [gate] }, { runId: 'run' }, { runId: 'run', gates: [{ ...gate, runId: 'other' }] }, { runId: 'run', gates: [gate, gate] }, { runId: 'run', gates: [{ ...gate, requiredApprovers: 0 }] }, { runId: 'run', gates: [{ ...gate, status: 'unknown' }] }, { runId: 'run', gates: [{ ...gate, votes: [{ note: {} }] }] },
  ])('does not authorize unknown, malformed or foreign gate evidence %j', (value) => {
    expect(readGateSnapshot(value, 'run')).toBeNull();
  });
  it('distinguishes accepted votes below quorum from completed gates and contradictory results', () => {
    expect(gateDecisionMessage('awaiting', 'approve')).toContain('still needs more');
    expect(gateDecisionMessage('approved', 'approve')).toBe('Approval gate approved.');
    expect(gateDecisionMessage('rejected', 'reject')).toBe('Approval gate rejected.');
    expect(gateDecisionMessage('rejected', 'approve')).toBeNull(); expect(gateDecisionMessage('unknown', 'approve')).toBeNull();
  });
  it('does not equate recorded promotion votes or policy approval with target deployment', () => {
    expect(promotionDecisionMessage('awaiting_approval', 'approve')).toContain('More approvals');
    expect(promotionDecisionMessage('approved', 'approve')).toContain('reported separately');
    expect(promotionDecisionMessage('approved', 'promote')).toContain('does not confirm deployment');
    expect(promotionDecisionMessage('failed', 'approve')).toBeNull(); expect(promotionDecisionMessage('unknown', 'promote')).toBeNull();
  });
});


describe('exact approval and promotion snapshots', () => {
  const expected: StageApproval = { id: 'gate', runId: 'run', stageId: 'stage', status: 'awaiting', requiredApprovers: 2, votes: [] };
  const next = { ...expected, votes: [{ approverEmail: 'owned@fixture.invalid', createdAt: '2026-10-02T00:00:00Z' }] };
  const decision = { runId: 'run', stageId: 'stage', status: 'awaiting', gate: next, approvalsHave: 1, approvalsNeed: 2 };
  const environments = [{ id: 'environment', name: 'Owned environment', order: 1, promotion: { strategy: 'manual', requiredApprovers: 2 } }];
  const statuses = { runId: 'run', statuses: [{ environmentId: 'environment', status: 'awaiting_approval', approvalsHave: 1, approvalsNeed: 2 }] };
  it('confirms the same retained gate and preserves its immutable threshold', () => {
    expect(confirmedGateDecision(decision, expected, 'approve')).toContain('still needs');
    expect(confirmedGateDecision({ ...decision, status: 'rejected', gate: { ...next, status: 'rejected' } }, expected, 'reject')).toBe('Approval gate rejected.');
  });
  it.each([
    null, { ...decision, runId: 'other' }, { ...decision, stageId: 'other' }, { ...decision, gate: { ...next, id: 'other' } }, { ...decision, gate: { ...next, runId: 'other' } }, { ...decision, status: 'approved' }, { ...decision, status: 'approved', gate: { ...next, status: 'approved' } }, { ...decision, approvalsHave: 2 }, { ...decision, approvalsNeed: 1 }, { ...decision, gate: { ...next, requiredApprovers: 1 } },
  ])('does not confirm a foreign, contradictory or insufficient gate receipt %j', (value) => {
    expect(confirmedGateDecision(value, expected, 'approve')).toBeNull();
  });
  it('accepts exact-run empty and populated promotion evidence without changing inputs', () => {
    expect(readPromotionSnapshot([], { runId: 'run', statuses: [] }, 'run')).toEqual({ environments: [], statuses: [] });
    const original = structuredClone({ environments, statuses }); expect(readPromotionSnapshot(environments, statuses, 'run')).toEqual({ environments, statuses: statuses.statuses }); expect({ environments, statuses }).toEqual(original);
  });
  it.each([
    [null, statuses], [environments, null], [environments, { runId: 'other', statuses: [] }], [environments, { runId: 'run', statuses: null }], [environments, []], [[...environments, ...environments], statuses], [[{ ...environments[0], promotion: { strategy: 'unknown' } }], statuses], [[{ ...environments[0], promotion: { strategy: { toString: 'auto' } } }], statuses], [environments, { runId: 'run', statuses: [{ ...statuses.statuses[0], status: 'unknown' }] }], [environments, { runId: 'run', statuses: [{ ...statuses.statuses[0], environmentId: 'other' }] }], [environments, { runId: 'run', statuses: [...statuses.statuses, ...statuses.statuses] }], [environments, { runId: 'run', statuses: [{ ...statuses.statuses[0], approvalsHave: -1 }] }],
  ])('rejects missing, foreign, duplicated or malformed promotion data %j', (envs, value) => {
    expect(readPromotionSnapshot(envs, value, 'run')).toBeNull();
  });
});
