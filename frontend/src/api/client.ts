import { getAccessToken, triggerSignIn } from '../auth/OIDCProvider';
import { API_ORIGIN } from './origin';

const API_BASE = `${API_ORIGIN}/api/v1`;

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

/**
 * requestText runs the same auth / 401 / 403 / error handling as
 * `request<T>` but speaks plain text instead of JSON. It is used by the
 * pipeline-as-code endpoints, which exchange `application/yaml`
 * documents rather than JSON. `contentType` lets the caller set the
 * request body's media type (the YAML import sends `application/yaml`);
 * the Accept header is supplied by the caller via `accept`.
 */
async function requestText(
  path: string,
  options: { method?: string; body?: string; contentType?: string; accept?: string } = {},
): Promise<string> {
  const headers: Record<string, string> = {};
  if (options.contentType) headers['Content-Type'] = options.contentType;
  if (options.accept) headers['Accept'] = options.accept;

  const token = getAccessToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(`${API_BASE}${path}`, {
    method: options.method ?? 'GET',
    headers,
    body: options.body,
  });

  if (res.status === 401) {
    triggerSignIn();
    throw new Error('Unauthorized');
  }

  if (res.status === 403) {
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
    // The error path is JSON even on text endpoints ({"error":...}); fall
    // back to statusText when the body isn't JSON.
    const error = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, error.error || `Request failed: ${res.status}`);
  }

  return res.text();
}

/** getText GETs a text/* resource (e.g. a YAML pipeline export). */
export function getText(path: string, accept?: string): Promise<string> {
  return requestText(path, { accept });
}

/**
 * postText POSTs a text/* body (e.g. a YAML pipeline import) and parses
 * the JSON response. The request body media type defaults to
 * `application/yaml`.
 */
export function postText<T>(path: string, body: string, contentType = 'application/yaml'): Promise<T> {
  return requestText(path, { method: 'POST', body, contentType }).then(
    (text) => JSON.parse(text) as T,
  );
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
