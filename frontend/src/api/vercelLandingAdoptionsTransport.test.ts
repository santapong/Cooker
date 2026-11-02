import { afterEach, expect, it, vi } from 'vitest';
import { triggerSignIn } from '../auth/OIDCProvider';
import { vercelLandingAdoptionsApi } from './vercelLandingAdoptions';

vi.mock('../auth/OIDCProvider', () => ({ getAccessToken: () => 'synthetic-adoption-token', triggerSignIn: vi.fn() }));

afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks(); });

it('passes cancellation to the authenticated create and versioned update requests', async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ adoption: {} }), { status: 200 }));
  vi.stubGlobal('fetch', fetch);
  const controller = new AbortController();
  const create = { connectionId: 'connection-1', connectionVersion: 4, projectId: 'prj_exact', provenancePublicationId: 'publication-1', confirmManualUpdates: true };
  await vercelLandingAdoptionsApi.create(create, controller.signal);
  expect(fetch.mock.calls[0][0]).toContain('/api/v1/admin/integrations/vercel/landing/adoptions');
  expect(fetch.mock.calls[0][1]).toMatchObject({ method: 'POST', signal: controller.signal, body: JSON.stringify(create), headers: { Authorization: 'Bearer synthetic-adoption-token' } });

  fetch.mockResolvedValueOnce(new Response(JSON.stringify({ adoption: {} }), { status: 200 }));
  const update = { version: 2, connectionVersion: 4, enabled: false, confirmManualUpdates: false };
  await vercelLandingAdoptionsApi.update('adoption-1', update, controller.signal);
  expect(fetch.mock.calls[1][0]).toContain('/api/v1/admin/integrations/vercel/landing/adoptions/adoption-1');
  expect(fetch.mock.calls[1][1]).toMatchObject({ method: 'PUT', signal: controller.signal, body: JSON.stringify(update) });
  expect(fetch).toHaveBeenCalledTimes(2);
});

it('ignores an aborted late response instead of redirecting a departed session', async () => {
  let resolveFetch!: (response: Response) => void;
  vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => { resolveFetch = resolve; })));
  const controller = new AbortController();
  const pending = vercelLandingAdoptionsApi.list(controller.signal);
  controller.abort();
  resolveFetch(new Response(JSON.stringify({ error: 'authentication required' }), { status: 401 }));
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  expect(triggerSignIn).not.toHaveBeenCalled();
});

it('does not start MFA from a response parsed after the adoption page was canceled', async () => {
  let resolveBody!: (body: unknown) => void;
  let bodyStarted!: () => void;
  const parsing = new Promise<void>((resolve) => { bodyStarted = resolve; });
  const body = new Promise<unknown>((resolve) => { resolveBody = resolve; });
  const response = { status: 403, clone: () => ({ json: () => { bodyStarted(); return body; } }) } as unknown as Response;
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response));
  const controller = new AbortController();
  const pending = vercelLandingAdoptionsApi.list(controller.signal);
  await parsing;
  controller.abort();
  resolveBody({ error: 'mfa_required', acr_values: ['mfa'] });
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  expect(triggerSignIn).not.toHaveBeenCalled();
});
