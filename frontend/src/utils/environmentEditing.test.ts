import { describe, expect, it } from 'vitest';
import { ApiError } from '../api/client';
import type { Environment } from '../types/environment';
import { configurationError, configurationText, knownEnvironmentKeys, ordinaryValues, parseConfiguration, toggleEnvironmentKey } from './environmentEditing';
const env: Environment = { id: 'env', name: 'Test', order: 1, createdAt: '', target: { type: 'namespace', clusterId: '', namespace: '', kubeContext: '' }, promotion: { strategy: 'manual' }, plainVars: { DATABASE_NAME: 'cooker', QUERY: ' x=y ' }, variables: { OLD: 'stale' }, secretKeys: ['PASSWORD'] };
describe('readable configuration and protected key separation', () => {
  it('preserves exact readable values and uses current plainVars over legacy variables', () => {
    expect(ordinaryValues(env)).toEqual(env.plainVars);
    expect(configurationText(env)).toBe('DATABASE_NAME=cooker\nQUERY= x=y ');
    expect(parseConfiguration(configurationText(env), env.secretKeys)).toEqual(env.plainVars);
    expect(knownEnvironmentKeys(env)).toEqual(['DATABASE_NAME', 'PASSWORD', 'QUERY']);
  });
  it('rejects protected collisions, malformed keys, duplicates and oversize without echoing values', () => {
    for (const input of ['PASSWORD=private-canary', 'A=private-canary\nA=again', 'wrong-key=private-canary', 'PRIVATE_CANARY', `A=${'x'.repeat(65536)}`]) {
      try { parseConfiguration(input, ['PASSWORD']); throw new Error('unexpected accept'); } catch (failure) { expect(String(failure)).not.toContain('private-canary'); expect(String(failure)).not.toContain('unexpected accept'); }
    }
  });
  it('round-trips multiline values with strict JSON rather than creating unintended keys', () => {
    const multiline = { ...env, plainVars: { MESSAGE: 'line one\nOTHER=value\r\nline three' } };
    expect(parseConfiguration(configurationText(multiline))).toEqual(multiline.plainVars);
    expect(() => parseConfiguration('{"A":"first","\\u0041":"second"}')).toThrow('once');
    expect(() => parseConfiguration('{"A":{"B":"value"}}')).toThrow('string values');
    expect(() => parseConfiguration('{"PASSWORD":"private-canary"}', ['PASSWORD'])).toThrow('protected key');
  });
  it('handles prototype-like keys as literal data', () => {
    const values = parseConfiguration('__proto__=literal\nconstructor=other');
    expect(Object.getPrototypeOf(values)).toBeNull();
    expect(values.__proto__).toBe('literal');
  });
  it('never exposes backend error canaries and distinguishes version and unsupported writes', () => {
    for (const status of [400, 403, 404, 409, 500, 501]) expect(configurationError(new ApiError(status, 'private-error-canary'))).not.toContain('private-error-canary');
    expect(configurationError(new ApiError(409, 'private'))).toContain('version changed');
    expect(configurationError(new ApiError(501, 'private'))).toContain('no unversioned retry');
  });
  it('selects only named keys without inventing or duplicating bindings', () => {
    expect(toggleEnvironmentKey(['PASSWORD'], 'PASSWORD', true)).toEqual(['PASSWORD']);
    expect(toggleEnvironmentKey(['PASSWORD'], 'DATABASE_NAME', true)).toEqual(['DATABASE_NAME', 'PASSWORD']);
    expect(toggleEnvironmentKey(['PASSWORD'], 'PASSWORD', false)).toEqual([]);
  });
});
