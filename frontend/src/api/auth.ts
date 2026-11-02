// Local-auth API surface. Mirrors backend handler/auth_local.go +
// server/auth_methods.go. Uses raw fetch (not the shared client)
// because signin/signup must work BEFORE the client has a token —
// the shared client triggers an OIDC redirect on 401, which breaks
// the local-auth path.

import { API_ORIGIN } from './origin';

const API_BASE = `${API_ORIGIN}/api/v1`;

export interface AuthMethods {
  oidc: { enabled: boolean; broker?: string; socialProviders?: string[] };
  local: { enabled: boolean; allowSignup: boolean };
}

export interface AuthUser {
  id: string;
  email: string;
  name: string;
  role: string;
  roles: string[];
}

export interface TokenResponse {
  token: string;
  expiresAt: string;
  user: AuthUser;
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || `Request failed: ${res.status}`);
  }
  return res.json();
}

export const authApi = {
  methods: async (): Promise<AuthMethods> => {
    const res = await fetch(`${API_BASE}/auth/methods`);
    if (!res.ok) {
      throw new Error('Sign-in methods are unavailable. Please try again.');
    }
    const result: AuthMethods = await res.json();
    if (typeof result?.oidc?.enabled !== 'boolean' || typeof result?.local?.enabled !== 'boolean' || typeof result?.local?.allowSignup !== 'boolean') {
      throw new Error('This server returned invalid sign-in methods.');
    }
    return result;
  },

  signin: (email: string, password: string): Promise<TokenResponse> =>
    postJSON('/auth/local/signin', { email, password }),

  signup: (email: string, password: string, name?: string): Promise<TokenResponse> =>
    postJSON('/auth/local/signup', { email, password, name }),

  me: async (token: string): Promise<AuthUser> => {
    const res = await fetch(`${API_BASE}/auth/local/me`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(err.error || `Request failed: ${res.status}`);
    }
    return res.json();
  },
};
