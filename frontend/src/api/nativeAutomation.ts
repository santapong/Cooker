import { get, post } from './client';
import type { TriggerRule } from './automation';

export interface NativeTriggerRule extends TriggerRule {
  appId: '';
  targetKind: 'pipeline';
  targetId: string;
  pipelineVersion?: number;
  reviewedAt?: string;
}
export interface NativeTriggerPreview {
  rule: NativeTriggerRule;
  envelope: { hash: string; commit: string; files: string[] };
}
export const nativeAutomationApi = {
  rules: () => get<(TriggerRule | NativeTriggerRule)[]>('/admin/trigger-rules'),
  preview: (rule: NativeTriggerRule, ref: string) => post<NativeTriggerPreview>('/admin/trigger-rules/preview', { rule, ref }),
  save: (preview: NativeTriggerPreview, ref: string) => post<NativeTriggerRule>('/admin/trigger-rules', { rule: preview.rule, ref, expectedEnvelopeHash: preview.envelope.hash, expectedPlanHash: preview.rule.planHash }),
  disable: (id: string) => post(`/admin/trigger-rules/${encodeURIComponent(id)}/disable`),
  check: (id: string, ref: string) => post(`/admin/trigger-rules/${encodeURIComponent(id)}/check`, { ref }),
};

function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

const strings = (value: unknown): value is string[] => Array.isArray(value) && value.every((item) => typeof item === 'string');
export function isNativeRuleFor(rule: unknown, pipelineId: string): rule is NativeTriggerRule {
  return record(rule) && rule.targetKind === 'pipeline' && rule.targetId === pipelineId && rule.appId === '' && typeof rule.id === 'string' && typeof rule.name === 'string' && Number.isSafeInteger(rule.version) && record(rule.source) && typeof rule.source.connectionId === 'string' && strings(rule.include) && (rule.exclude === undefined || strings(rule.exclude)) && (rule.refKind === 'branch' || rule.refKind === 'tag') && (rule.match === 'exact' || rule.match === 'glob') && (rule.approval === 'review' || rule.approval === 'automatic');
}

export function nativePreviewUsable(preview: unknown, pipelineId: string, pipelineVersion?: number): preview is NativeTriggerPreview {
  if (!record(preview) || !isNativeRuleFor(preview.rule, pipelineId) || !record(preview.envelope)) return false;
  const envelope = preview.envelope;
  return Number.isSafeInteger(pipelineVersion) && (pipelineVersion ?? 0) > 0 && preview.rule.pipelineVersion === pipelineVersion && typeof preview.rule.planHash === 'string' && /^[a-f0-9]{64}$/.test(preview.rule.planHash) && typeof envelope.hash === 'string' && /^[a-f0-9]{64}$/.test(envelope.hash) && preview.rule.approvedEnvelopeHash === envelope.hash && typeof envelope.commit === 'string' && /^[a-f0-9]{40}([a-f0-9]{24})?$/.test(envelope.commit) && strings(envelope.files);
}
