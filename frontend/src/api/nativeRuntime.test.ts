import { afterEach, expect, it, vi } from 'vitest';
vi.mock('../auth/OIDCProvider', () => ({ getAccessToken: () => 'owned-native-fixture-token', triggerSignIn: vi.fn() }));
import { nativeRuntimeApi, nativeRuntimeLogsPath } from './nativeRuntime';
import type { NativeRuntimeSubject } from '../types/nativeRuntime';

afterEach(() => vi.unstubAllGlobals());
const subject: NativeRuntimeSubject = { kind: 'native-stage', pipelineId: 'p%41:ไทย', runId: 'run:one', stageId: 'web%2F' };

it('keeps native identity segments encoded once for HTTP and ticket paths', async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ binding: 'unavailable' }), { status: 200 }));
  vi.stubGlobal('fetch', fetch);
  await nativeRuntimeApi.status(subject);
  const path = '/pipelines/p%2541%3A%E0%B9%84%E0%B8%97%E0%B8%A2/runs/run%3Aone/stages/web%252F';
  expect(fetch.mock.calls[0][0]).toContain(`${path}/runtime`);
  expect(nativeRuntimeLogsPath(subject)).toBe(`/ws${path}/runtime-logs`);
  expect(fetch.mock.calls[0][1].headers.Authorization).toBe('Bearer owned-native-fixture-token');
});

it('authenticates native downloads and closes only the encoded subject session', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(new Response('known value [REDACTED]\n')).mockResolvedValueOnce(new Response(null, { status: 204 }));
  vi.stubGlobal('fetch', fetch);
  expect(await nativeRuntimeApi.download(subject, 'session:one')).toBe('known value [REDACTED]\n');
  expect(fetch.mock.calls[0][0]).toContain('/log-sessions/session%3Aone/download');
  expect(fetch.mock.calls[0][1].headers).toMatchObject({ Authorization: 'Bearer owned-native-fixture-token', Accept: 'text/plain' });
  await expect(nativeRuntimeApi.close(subject, 'session:one')).resolves.toBeUndefined();
  expect(fetch.mock.calls[1][1].method).toBe('DELETE');
});
