import { afterEach, expect, it, vi } from 'vitest';
vi.mock('../auth/OIDCProvider', () => ({ getAccessToken: () => 'owned-fixture-token', triggerSignIn: vi.fn() }));
import { environmentsApi } from './environments';
afterEach(() => vi.unstubAllGlobals());
it('reads metadata and version-checks protected writes without reveal requests', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ keys: ['PASSWORD'], sourceAvailability: 'available', generation: 8, conditionalEditing: true }))).mockResolvedValueOnce(new Response(JSON.stringify({ status: 'stored', generation: 9 }))).mockResolvedValueOnce(new Response(JSON.stringify({ status: 'deleted', generation: 10 })));
  vi.stubGlobal('fetch', fetch);
  await environmentsApi.secretKeys('env/one'); await environmentsApi.putSecret('env/one', 'PASSWORD?', 'synthetic-value', 8); await environmentsApi.deleteSecret('env/one', 'PASSWORD?', 9);
  expect(fetch.mock.calls.map((call) => call[0])).toEqual(['/api/v1/environments/env%2Fone/secret-keys', '/api/v1/environments/env%2Fone/secrets/PASSWORD%3F', '/api/v1/environments/env%2Fone/secrets/PASSWORD%3F?version=9']);
  expect(fetch.mock.calls[1][1]).toMatchObject({ method: 'PUT', body: JSON.stringify({ value: 'synthetic-value', version: 8 }) });
  expect(fetch.mock.calls[2][1].method).toBe('DELETE');
});
