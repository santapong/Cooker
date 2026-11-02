import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AuthMethods } from '../api/auth';
import { canSignInWithProvider, isOIDCSignInConfigured, redirectToSocialProvider, safeAuthReturnPath, type SocialProvider } from './socialSignIn';

afterEach(() => vi.unstubAllEnvs());

const enabled: AuthMethods = {
  oidc: { enabled: true, broker: 'keycloak', socialProviders: ['github', 'google'] },
  local: { enabled: true, allowSignup: true },
};

describe('brokered social sign-in', () => {
  it.each([
    ['false', 'https://id.fixture.invalid', 'cooker', false],
    ['true', '', 'cooker', false],
    ['true', 'https://id.fixture.invalid', '', false],
    ['true', 'https://id.fixture.invalid', 'cooker', true],
  ])('requires matching frontend OIDC inputs', (enabled, authority, client, expected) => {
    vi.stubEnv('VITE_OIDC_ENABLED', enabled as string);
    vi.stubEnv('VITE_OIDC_AUTHORITY', authority as string);
    vi.stubEnv('VITE_OIDC_CLIENT_ID', client as string);
    expect(isOIDCSignInConfigured()).toBe(expected);
  });
  it.each(['github', 'google'] as const)('selects only the %s alias and preserves the local destination', async (provider) => {
    const signinRedirect = vi.fn().mockResolvedValue(undefined);
    await redirectToSocialProvider({ signinRedirect }, enabled, provider, '/pipelines/synced/vercel-123?environment=production#history');
    expect(signinRedirect).toHaveBeenCalledTimes(1);
    expect(signinRedirect).toHaveBeenCalledWith({ extraQueryParams: { kc_idp_hint: provider }, state: { returnTo: '/pipelines/synced/vercel-123?environment=production#history' } });
  });

  it.each([
    { oidc: { enabled: false, broker: 'keycloak', socialProviders: ['github'] }, local: enabled.local },
    { oidc: { enabled: true, broker: 'other', socialProviders: ['github'] }, local: enabled.local },
    { oidc: { enabled: true, broker: 'keycloak', socialProviders: ['google'] }, local: enabled.local },
    { oidc: { enabled: true }, local: enabled.local },
  ])('never redirects for an unavailable or older-server provider', async (methods) => {
    const signinRedirect = vi.fn();
    await expect(redirectToSocialProvider({ signinRedirect }, methods, 'github', '/')).rejects.toThrow('not configured');
    expect(signinRedirect).not.toHaveBeenCalled();
  });

  it('rejects missing frontend OIDC configuration and unknown provider hints', async () => {
    expect(canSignInWithProvider(enabled, false, 'github')).toBe(false);
    await expect(redirectToSocialProvider(null, enabled, 'github', '/')).rejects.toThrow();
    const signinRedirect = vi.fn();
    await expect(redirectToSocialProvider({ signinRedirect }, enabled, 'attacker' as SocialProvider, '/')).rejects.toThrow();
    expect(signinRedirect).not.toHaveBeenCalled();
  });

  it('propagates redirect failures so the sign-in form can recover', async () => {
    const signinRedirect = vi.fn().mockRejectedValue(new Error('Provider unavailable'));
    await expect(redirectToSocialProvider({ signinRedirect }, enabled, 'google', '/')).rejects.toThrow('Provider unavailable');
  });
});

describe('safe sign-in return navigation', () => {
  it.each([undefined, null, {}, 'https://evil.example', '//evil.example', '/\\evil.example', '/\nevil.example', '/callback?code=x', '/signin', '/signup#again', '/foo/../callback'])('rejects external, invalid or looping destinations: %s', (value) => {
    expect(safeAuthReturnPath(value)).toBe('/');
  });
  it('retains valid local paths, filters and fragments', () => {
    expect(safeAuthReturnPath('/apps?search=my-app#services')).toBe('/apps?search=my-app#services');
  });
});
