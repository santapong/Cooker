/**
 * Split-origin helpers.
 *
 * When VITE_API_BASE_URL is set the SPA and the API are served from
 * different origins (e.g. SPA on Vercel, API elsewhere).  When it is
 * unset every URL is relative and the single-binary same-origin
 * behaviour is preserved byte-for-byte.
 *
 * All HTTP and WebSocket paths in the frontend MUST derive their origin
 * from these helpers — never from window.location directly — so that
 * split-origin wiring is a single-place change.
 *
 * Rules:
 *   - apiOrigin() returns '' (empty) when VITE_API_BASE_URL is unset,
 *     so that `${apiOrigin()}/api/v1/...` stays a plain relative path.
 *   - wsBase()    returns the correct WS authority string, e.g.
 *     "wss://api.example.com" or (fallback) "ws://localhost:8080".
 *     The caller appends the path, e.g. `${wsBase()}/ws/runs/...`.
 *
 * The _derive* functions are pure helpers exported only for unit tests.
 * Application code should use the pre-bound API_ORIGIN constant and
 * wsBase() which already read from import.meta.env.
 */

/** Trailing-slash-stripped API origin, or '' for same-origin. */
export const API_ORIGIN: string = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/+$/, '');

/**
 * _deriveWsBase is the pure, side-effect-free core of wsBase().
 * It is exported only for unit testing — application code uses wsBase().
 *
 * @param apiBaseUrl  The value of VITE_API_BASE_URL (may be '' or undefined).
 * @param windowProto window.location.protocol at call time (e.g. 'https:').
 * @param windowHost  window.location.host at call time (e.g. 'app.example.com').
 */
export function _deriveWsBase(
  apiBaseUrl: string | undefined,
  windowProto: string,
  windowHost: string,
): string {
  const stripped = (apiBaseUrl ?? '').replace(/\/+$/, '');
  if (stripped) {
    const proto = stripped.startsWith('https://') ? 'wss:' : 'ws:';
    const authority = stripped.replace(/^https?:\/\//, '');
    return `${proto}//${authority}`;
  }
  // Same-origin fallback: mirror window.location.
  const proto = windowProto === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${windowHost}`;
}

/**
 * wsBase derives the WebSocket authority from VITE_API_BASE_URL when
 * set, otherwise falls back to window.location.
 *
 * Examples:
 *   VITE_API_BASE_URL=https://api.example.com  →  "wss://api.example.com"
 *   VITE_API_BASE_URL=http://localhost:8080     →  "ws://localhost:8080"
 *   (unset)                                     →  "wss://app.example.com" (from window)
 *
 * The caller is responsible for appending the path, e.g.:
 *   `${wsBase()}/ws/runs/${runId}`
 */
export function wsBase(): string {
  return _deriveWsBase(
    import.meta.env.VITE_API_BASE_URL,
    typeof window !== 'undefined' ? window.location.protocol : 'https:',
    typeof window !== 'undefined' ? window.location.host : 'localhost',
  );
}
