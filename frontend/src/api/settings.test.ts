import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { settingsApi } from './settings';
import { ApiError } from './client';
import { triggerSignIn } from '../auth/OIDCProvider';

vi.mock('../auth/OIDCProvider', () => ({ getAccessToken: () => 'owned-fixture-session', triggerSignIn: vi.fn() }));
const fetchMock = vi.fn();
beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset(); vi.mocked(triggerSignIn).mockClear(); });
afterEach(() => vi.unstubAllGlobals());

it('returns the redacted created registry and sends credentials only in the create request', async () => {
  const registry = { id: 'registry-a', name: 'GHCR', url: 'ghcr.io', version: 7, hasPassword: true };
  fetchMock.mockResolvedValue(Response.json(registry));
  expect(await settingsApi.addRegistry({ name: 'GHCR', url: 'ghcr.io', password: 'owned-create-fixture' })).toEqual(registry);
  const [url, request] = fetchMock.mock.calls[0];
  expect(url).toMatch(/\/settings\/registries$/);
  expect(request.method).toBe('POST');
  expect(JSON.parse(request.body)).toEqual({ name: 'GHCR', url: 'ghcr.io', password: 'owned-create-fixture' });
});

it('patches the exact encoded connection with both explicit grant lists and its observed version', async () => {
  const change = { version: 7, allowedAppIds: [], allowedPipelineIds: ['native-pipeline'] };
  fetchMock.mockResolvedValue(Response.json({ id: 'registry/a?', version: 8, allowedPipelineIds: ['native-pipeline'] }));
  expect((await settingsApi.updateRegistryGrants('registry/a?', change)).version).toBe(8);
  const [url, request] = fetchMock.mock.calls[0];
  expect(url).toMatch(/\/settings\/registries\/registry%2Fa%3F\/grants$/);
  expect(request.method).toBe('PATCH');
  expect(request.headers.Authorization).toBe('Bearer owned-fixture-session');
  expect(JSON.parse(request.body)).toEqual(change);
  expect(Object.keys(JSON.parse(request.body))).toEqual(['version', 'allowedAppIds', 'allowedPipelineIds']);
});

it('preserves grant conflicts for explicit reload without retrying a mutation', async () => {
  fetchMock.mockResolvedValue(Response.json({ error: 'changed' }, { status: 409 }));
  try { await settingsApi.updateRegistryGrants('r', { version: 7, allowedAppIds: [], allowedPipelineIds: [] }); throw new Error('accepted stale edit'); }
  catch (error) { expect(error).toBeInstanceOf(ApiError); expect((error as ApiError).status).toBe(409); }
  expect(fetchMock).toHaveBeenCalledTimes(1);
});

it('keeps the existing MFA challenge policy for grant PATCH', async () => {
  fetchMock.mockResolvedValue(Response.json({ error: 'mfa_required', acr_values: ['mfa'] }, { status: 403 }));
  await expect(settingsApi.updateRegistryGrants('r', { version: 7, allowedAppIds: [], allowedPipelineIds: [] })).rejects.toThrow('MFA required');
  expect(triggerSignIn).toHaveBeenCalledWith({ acrValues: 'mfa' });
  expect(fetchMock).toHaveBeenCalledTimes(1);
});
