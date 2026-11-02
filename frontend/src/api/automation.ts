import { get, post } from './client';
import type { ComposeGraph } from '../types/compose';

export interface SourceConnection {
  id: string; name: string; provider: 'github' | 'gitlab' | 'gitea';
  origin: string; apiOrigin: string; enabled: boolean; version: number;
  repositories: string[]; authMode: string; username?: string; installationId?: number;
  webhookAuthMode: string; hasToken: boolean; hasWebhookSecret: boolean;
}

/** Validate the public metadata before using it as a replacement acknowledgement. */
export function validSourceConnectionMetadata(value: unknown): value is SourceConnection {
  if (!value || typeof value !== 'object') return false;
  const c = value as Record<string, unknown>;
  return ['id', 'name', 'origin', 'apiOrigin'].every((key) => typeof c[key] === 'string' && c[key] !== '')
    && typeof c.provider === 'string' && ['github', 'gitlab', 'gitea'].includes(c.provider)
    && typeof c.authMode === 'string' && ['anonymous', 'token', 'github_app'].includes(c.authMode)
    && typeof c.webhookAuthMode === 'string' && ['signing', 'legacy_token'].includes(c.webhookAuthMode)
    && typeof c.version === 'number' && Number.isInteger(c.version) && c.version > 0
    && ['enabled', 'hasToken', 'hasWebhookSecret'].every((key) => typeof c[key] === 'boolean')
    && (c.username === undefined || typeof c.username === 'string')
    && Array.isArray(c.repositories) && c.repositories.length > 0 && c.repositories.length <= 100
    && c.repositories.every((repo) => typeof repo === 'string' && repo !== '');
}
export interface TriggerRule {
  id: string; version: number; name: string; enabled: boolean; appId: string;
  source: { connectionId: string; provider?: string; host?: string; repositoryId?: string; repository?: string };
  refKind: 'branch' | 'tag'; match: 'exact' | 'glob'; include: string[]; exclude: string[];
  approval: 'automatic' | 'review'; planHash?: string; approvedEnvelopeHash?: string;
}
export interface TriggerPreview {
  rule: TriggerRule;
  envelope: { hash: string; commit: string; files: string[]; graph: ComposeGraph };
}
export interface TriggerIntent {
  id: string; status: string; reason: string; rule: TriggerRule;
  event: { fullRef: string; commitSha: string };
  receipt?: { runId: string; pipelineId: string };
}
export const automationApi = {
  appConnections: () => get<SourceConnection[]>('/apps/source-connections'),
  connections: () => get<SourceConnection[]>('/admin/source-connections'),
  connect: (input: Record<string, unknown>) => post<SourceConnection>('/admin/source-connections', input),
  replaceToken: (id: string, input: { version: number; token: string; username?: string }) => post<{ connection: SourceConnection; checkedRepositories: number; check: 'repository_metadata_access' }>(`/admin/source-connections/${encodeURIComponent(id)}/token-replacement`, input),
  rules: () => get<TriggerRule[]>('/admin/trigger-rules'),
  preview: (rule: TriggerRule, ref: string) => post<TriggerPreview>('/admin/trigger-rules/preview', { rule, ref }),
  save: (preview: TriggerPreview, ref: string) => post<TriggerRule>('/admin/trigger-rules', { rule: preview.rule, ref, expectedEnvelopeHash: preview.envelope.hash, expectedPlanHash: preview.rule.planHash }),
  disable: (id: string) => post<TriggerRule>(`/admin/trigger-rules/${encodeURIComponent(id)}/disable`),
  check: (id: string, ref: string) => post(`/admin/trigger-rules/${encodeURIComponent(id)}/check`, { ref }),
  intents: (appId: string) => get<TriggerIntent[]>(`/admin/trigger-intents?appId=${encodeURIComponent(appId)}`),
  approve: (id: string) => post(`/admin/trigger-intents/${encodeURIComponent(id)}/approve`),
};
