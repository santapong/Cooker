import type { UserManager } from 'oidc-client-ts';
import type { AuthMethods } from '../api/auth';

export const SOCIAL_PROVIDERS = ['github', 'google'] as const;
export type SocialProvider = typeof SOCIAL_PROVIDERS[number];

export function isOIDCSignInConfigured(): boolean {
  return import.meta.env.VITE_OIDC_ENABLED === 'true' && !!import.meta.env.VITE_OIDC_AUTHORITY && !!import.meta.env.VITE_OIDC_CLIENT_ID;
}

/** Only local destinations survive the provider round trip. */
export function safeAuthReturnPath(value: unknown): string {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')
    || Array.from(value).some((character) => character.charCodeAt(0) <= 32 || character.charCodeAt(0) === 127)) return '/';
  try {
    const url = new URL(value, 'https://cooker.invalid');
    if (url.origin !== 'https://cooker.invalid' || ['/signin', '/signup', '/callback'].includes(url.pathname)) return '/';
    return url.pathname + url.search + url.hash;
  } catch {
    return '/';
  }
}

export function canSignInWithProvider(methods: AuthMethods, oidcReady: boolean, provider: SocialProvider): boolean {
  return oidcReady && methods.oidc.enabled && methods.oidc.broker === 'keycloak'
    && Array.isArray(methods.oidc.socialProviders) && methods.oidc.socialProviders.includes(provider);
}

export async function redirectToSocialProvider(manager: Pick<UserManager, 'signinRedirect'> | null, methods: AuthMethods, provider: SocialProvider, returnTo: unknown): Promise<void> {
  if (!SOCIAL_PROVIDERS.includes(provider) || !canSignInWithProvider(methods, manager !== null, provider)) {
    throw new Error('This sign-in provider is not configured.');
  }
  // oidc-client-ts owns code/PKCE, nonce and state verification. The hint
  // selects a trusted Keycloak alias; it never changes issuer or callback.
  await manager!.signinRedirect({
    extraQueryParams: { kc_idp_hint: provider },
    state: { returnTo: safeAuthReturnPath(returnTo) },
  });
}
