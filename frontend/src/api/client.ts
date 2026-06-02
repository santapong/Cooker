import { getAccessToken, triggerSignIn } from '../auth/OIDCProvider';

const API_BASE = '/api/v1';

/**
 * ApiError is thrown for any non-2xx response that isn't handled inline
 * (401 sign-in redirect, MFA 403). It carries the HTTP `status` so
 * callers can branch on it — e.g. the Kubernetes store treats only 503
 * as "cluster unavailable" and everything else (403/500/timeout) as a
 * plain error. The `message` is still the backend's `error` field (or a
 * sensible fallback), so existing `(e as Error).message` consumers are
 * unaffected.
 */
export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options?.headers as Record<string, string> | undefined),
  };

  const token = getAccessToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers,
  });

  if (res.status === 401) {
    triggerSignIn();
    throw new Error('Unauthorized');
  }

  if (res.status === 403) {
    // Step-up MFA challenge: backend returns
    //   {"error":"mfa_required","acr_values":["mfa", ...]}
    // Re-issue the sign-in redirect with acr_values so the IdP runs
    // the second factor.
    const body = await res
      .clone()
      .json()
      .catch(() => null);
    if (body?.error === 'mfa_required' && Array.isArray(body.acr_values) && body.acr_values.length > 0) {
      triggerSignIn({ acrValues: body.acr_values.join(' ') });
      throw new Error('MFA required');
    }
  }

  if (!res.ok) {
    const error = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, error.error || `Request failed: ${res.status}`);
  }

  return res.json();
}

export function get<T>(path: string): Promise<T> {
  return request<T>(path);
}

export function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: 'POST',
    body: body ? JSON.stringify(body) : undefined,
  });
}

export function put<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, {
    method: 'PUT',
    body: JSON.stringify(body),
  });
}

export function del<T>(path: string): Promise<T> {
  return request<T>(path, { method: 'DELETE' });
}
