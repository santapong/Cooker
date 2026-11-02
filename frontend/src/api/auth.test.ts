import { afterEach, describe, expect, it, vi } from 'vitest';
import { authApi } from './auth';

afterEach(() => vi.unstubAllGlobals());

describe('sign-in method discovery', () => {
  it.each([404, 500, 503])('does not invent an enabled sign-in method on HTTP %s', async (status) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('unavailable', { status })));
    await expect(authApi.methods()).rejects.toThrow('unavailable');
  });
  it('accepts a successful older response without enabling social providers', async () => {
    const methods = { oidc: { enabled: true }, local: { enabled: false, allowSignup: false } };
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(methods)));
    expect(await authApi.methods()).toEqual(methods);
  });
  it('rejects malformed capability data', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ oidc: { enabled: 'true' } })));
    await expect(authApi.methods()).rejects.toThrow('invalid');
  });
});
