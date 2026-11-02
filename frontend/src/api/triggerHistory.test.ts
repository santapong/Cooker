import { expect, it, vi } from 'vitest';
import { historyQuery, triggerHistoryApi, webhookAttemptScopeQuery } from './triggerHistory';
import { get } from './client';
vi.mock('./client', () => ({ get: vi.fn().mockResolvedValue({ items: [], total: 0 }) }));

it('encodes exact refs and literal search text while retaining pagination', async () => {
  const query = historyQuery({ ref: 'refs/tags/release/1', q: 'shop%_ & main', limit: 25, offset: 100, provider: '' });
  const parsed = new URLSearchParams(query);
  expect(parsed.get('q')).toBe('shop%_ & main'); expect(parsed.get('ref')).toBe('refs/tags/release/1');
  expect(parsed.get('offset')).toBe('100'); expect(parsed.has('provider')).toBe(false);
  await triggerHistoryApi.delivery('id/with?reserved', { status: 'duplicate' });
  expect(get).toHaveBeenLastCalledWith('/admin/trigger-deliveries/id%2Fwith%3Freserved/history?status=duplicate');
  await triggerHistoryApi.attempts({ status: 'received', appId: 'native', offset: 25 });
  expect(get).toHaveBeenLastCalledWith('/admin/webhook-attempts?status=received&appId=native&offset=25');
});

it('keeps native pipeline scope separate from App IDs', async () => {
  await triggerHistoryApi.list({ targetKind: 'pipeline', targetId: 'native-pipeline', limit: 25, offset: 0 });
  expect(get).toHaveBeenLastCalledWith('/admin/trigger-deliveries?targetKind=pipeline&targetId=native-pipeline&limit=25&offset=0');
  expect(historyQuery({ targetKind: 'pipeline', targetId: 'native-pipeline' })).not.toContain('appId=');
});

it('reads native source attempts before target decisions without fabricating an App or pipeline association', async () => {
  const scope = webhookAttemptScopeQuery('connection', undefined, 'source/& one');
  await triggerHistoryApi.attempts({ ...scope, status: 'uncertain', q: 'original & retry', limit: 25, offset: 25 });
  const calls = vi.mocked(get).mock.calls;
  const requested = new URLSearchParams(calls[calls.length - 1][0].split('?')[1]);
  expect(requested.get('connectionId')).toBe('source/& one');
  expect(requested.get('status')).toBe('uncertain');
  expect(requested.get('q')).toBe('original & retry');
  expect(requested.get('offset')).toBe('25');
  expect(requested.has('appId')).toBe(false);
  expect(requested.has('targetKind')).toBe(false);
  expect(requested.has('targetId')).toBe(false);
});

it('keeps existing App and explicitly selected all-request scopes', () => {
  expect(webhookAttemptScopeQuery('app', 'app1', 'source1')).toEqual({ appId: 'app1' });
  expect(webhookAttemptScopeQuery('all', 'app1', 'source1')).toEqual({});
});

it('does not widen a missing scoped identity into all incoming requests', () => {
  expect(() => webhookAttemptScopeQuery('connection')).toThrow('Source connection unavailable');
  expect(() => webhookAttemptScopeQuery('app')).toThrow('App unavailable');
});
