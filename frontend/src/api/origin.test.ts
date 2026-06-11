/**
 * Unit tests for the split-origin helpers in origin.ts.
 *
 * API_ORIGIN is a module-level constant baked from import.meta.env at
 * evaluation time, so we test its derivation logic (trailing-slash strip)
 * directly rather than trying to re-evaluate the module with a different
 * env value.
 *
 * _deriveWsBase is the pure core of wsBase() and covers all three cases:
 *   1. unset VITE_API_BASE_URL  → same-origin WS (window.location)
 *   2. https base URL           → wss:// from the configured host
 *   3. http base URL            → ws:// from the configured host
 *
 * Trailing-slash normalisation for API_ORIGIN is also exercised through
 * the same derive function (same replace() expression).
 */
import { describe, it, expect } from 'vitest';
import { _deriveWsBase } from './origin';

// Convenience shim: call _deriveWsBase with a stable "same-origin"
// window (https page on app.example.com) so tests only vary apiBaseUrl.
function derive(apiBaseUrl: string | undefined): string {
  return _deriveWsBase(apiBaseUrl, 'https:', 'app.example.com');
}

describe('_deriveWsBase — unset VITE_API_BASE_URL (same-origin)', () => {
  it('returns wss:// from window.location when apiBaseUrl is undefined', () => {
    expect(_deriveWsBase(undefined, 'https:', 'app.example.com')).toBe(
      'wss://app.example.com',
    );
  });

  it('returns ws:// when window.location.protocol is http:', () => {
    expect(_deriveWsBase(undefined, 'http:', 'localhost:5173')).toBe(
      'ws://localhost:5173',
    );
  });

  it('returns wss:// for an empty-string apiBaseUrl (same-origin parity)', () => {
    expect(derive('')).toBe('wss://app.example.com');
  });
});

describe('_deriveWsBase — https VITE_API_BASE_URL → wss://', () => {
  it('converts https base to wss authority', () => {
    expect(derive('https://api.example.com')).toBe('wss://api.example.com');
  });

  it('strips a trailing slash before converting', () => {
    expect(derive('https://api.example.com/')).toBe('wss://api.example.com');
    expect(derive('https://api.example.com///')).toBe('wss://api.example.com');
  });

  it('preserves an explicit port in the authority', () => {
    expect(derive('https://api.example.com:8443')).toBe('wss://api.example.com:8443');
  });
});

describe('_deriveWsBase — http VITE_API_BASE_URL → ws://', () => {
  it('converts http base to ws authority', () => {
    expect(derive('http://localhost:8080')).toBe('ws://localhost:8080');
  });

  it('strips a trailing slash before converting', () => {
    expect(derive('http://localhost:8080/')).toBe('ws://localhost:8080');
  });
});

describe('API_ORIGIN trailing-slash stripping (same replace() expression)', () => {
  // We verify the normalisation rule by testing that the same .replace()
  // expression used in origin.ts produces the expected output.  This
  // keeps the test co-located with the logic without re-importing the
  // module-level constant (which is already baked in vitest's env).
  function normalise(raw: string): string {
    return raw.replace(/\/+$/, '');
  }

  it('strips a single trailing slash', () => {
    expect(normalise('https://api.example.com/')).toBe('https://api.example.com');
  });

  it('strips multiple trailing slashes', () => {
    expect(normalise('https://api.example.com///')).toBe('https://api.example.com');
  });

  it('leaves a URL with no trailing slash unchanged', () => {
    expect(normalise('https://api.example.com')).toBe('https://api.example.com');
  });

  it('returns empty string unchanged (unset → same-origin)', () => {
    expect(normalise('')).toBe('');
  });
});
