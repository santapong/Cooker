import { get } from './client';

export const historyStatuses = ['accepted', 'ignored', 'duplicate', 'stale', 'rejected', 'review_required', 'failed', 'archived', 'unknown'] as const;
export type HistoryStatus = typeof historyStatuses[number];
export interface TriggerHistoryRow {
  deliveryId: string; providerDeliveryId: string; provider: string; connectionId: string;
  repository?: string; fullRef?: string; commitSha?: string; deleted: boolean;
  ruleId?: string; ruleVersion?: number; appId?: string; environmentId?: string; targetKind?: string; targetId?: string;
  targetSource?: 'intent' | 'current_rule' | 'decision'; metadataCoverage?: 'retained_decision' | 'legacy';
  intentId?: string; intentStatus?: string; jobId?: string; jobStatus?: string;
  runId?: string; runStatus?: string; pipelineId?: string;
  status: HistoryStatus; reasonCode: string; reason: string;
  createdAt: string; updatedAt: string; archived: boolean; decisionIndex: number;
}
export interface TriggerHistoryPage {
  items: TriggerHistoryRow[]; total: number; limit: number; offset: number;
  coverage: string; limitations: string[];
  journalStartedAt?: string;
}
export interface TriggerHistoryQuery {
  appId?: string; provider?: string; connectionId?: string; status?: string;
  ref?: string; q?: string; deliveryId?: string; limit?: number; offset?: number;
  targetKind?: 'app' | 'pipeline'; targetId?: string;
}
export function historyQuery(query: TriggerHistoryQuery): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) if (value !== undefined && value !== '') search.set(key, String(value));
  return search.toString();
}
export const triggerHistoryApi = {
  list: (query: TriggerHistoryQuery) => get<TriggerHistoryPage>(`/admin/trigger-deliveries?${historyQuery(query)}`),
  delivery: (id: string, query: TriggerHistoryQuery = {}) => get<TriggerHistoryPage>(`/admin/trigger-deliveries/${encodeURIComponent(id)}/history?${historyQuery(query)}`),
  attempts: (query: Omit<TriggerHistoryQuery, 'ref'>) => get<WebhookAttemptPage>(`/admin/webhook-attempts?${historyQuery(query)}`),
};

export interface WebhookAttempt {
  id: string; provider: string; connectionId: string; providerDeliveryId?: string; deliveryId?: string;
  status: 'received' | 'accepted' | 'ignored' | 'duplicate' | 'rejected' | 'failed' | 'uncertain';
  reasonCode: string; createdAt: string; finishedAt?: string;
}
export interface WebhookAttemptPage {
  items: WebhookAttempt[]; total: number; limit: number; offset: number; journalStartedAt: string; coverage: string; limitations: string[];
}

export type WebhookAttemptScope = 'all' | 'app' | 'connection';
/** An attempt can precede any target decision; never infer a pipeline target. */
export function webhookAttemptScopeQuery(scope: WebhookAttemptScope, appId?: string, connectionId?: string): Pick<TriggerHistoryQuery, 'appId' | 'connectionId'> {
  if (scope === 'connection') {
    if (!connectionId) throw new Error('Source connection unavailable');
    return { connectionId };
  }
  if (scope === 'app') {
    if (!appId) throw new Error('App unavailable');
    return { appId };
  }
  return {};
}
