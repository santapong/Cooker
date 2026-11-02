import { afterEach, expect, it, vi } from 'vitest';
vi.mock('../auth/OIDCProvider', () => ({ getAccessToken: () => 'owned-fixture-token', triggerSignIn: vi.fn() }));
import { runtimeLogsApi } from './runtimeLogs';
afterEach(() => vi.unstubAllGlobals());
it('encodes service/session identity, authenticates download and accepts an empty close response', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ replicas: [] }), { status: 200 })).mockResolvedValueOnce(new Response('known value [REDACTED]\n', { status: 200 })).mockResolvedValueOnce(new Response(null, { status: 204 }));
  vi.stubGlobal('fetch', fetch);
  await runtimeLogsApi.replicas('app/one', 'web?one');
  expect(fetch.mock.calls[0][0]).toContain('/apps/app%2Fone/services/web%3Fone/log-replicas');
  expect(await runtimeLogsApi.download('app/one', 'web?one', 'session/one')).toBe('known value [REDACTED]\n');
  expect(fetch.mock.calls[1][0]).toContain('/log-sessions/session%2Fone/download');
  expect(fetch.mock.calls[1][1].headers).toMatchObject({ Authorization: 'Bearer owned-fixture-token', Accept: 'text/plain' });
  await expect(runtimeLogsApi.close('app/one', 'web?one', 'session/one')).resolves.toBeUndefined();
  expect(fetch.mock.calls[2][1].method).toBe('DELETE');
});
