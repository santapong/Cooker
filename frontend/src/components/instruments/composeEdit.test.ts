import { describe, expect, it } from 'vitest';
import { envToLines, parseEnvLines, parseList, sameEnv, sameList } from './composeEdit';

describe('compose service editor helpers', () => {
  it('parses one entry per line and drops blanks', () => {
    expect(parseList(' 8080:80 \n\n9000:9000\n')).toEqual(['8080:80', '9000:9000']);
  });

  it('round-trips KEY=value lines, keeping "=" inside values', () => {
    const env = { DATABASE_URL: 'postgres://db/acme?sslmode=disable', EMPTY: '' };
    const text = envToLines(env);
    expect(text).toBe('DATABASE_URL=postgres://db/acme?sslmode=disable\nEMPTY=');
    expect(parseEnvLines(text)).toEqual({ env });
  });

  it('reports the first malformed line by number', () => {
    expect(parseEnvLines('OK=1\n\n2BAD=x')).toEqual({ error: 'Line 3: "2BAD" is not a valid variable name.' });
    expect(parseEnvLines('=value')).toEqual({ error: 'Line 1: "=value" is not a valid variable name.' });
  });

  it('a bare KEY line means an empty value', () => {
    expect(parseEnvLines('FLAG')).toEqual({ env: { FLAG: '' } });
  });

  it('compares lists and env maps structurally', () => {
    expect(sameList(['a', 'b'], ['a', 'b'])).toBe(true);
    expect(sameList(['a', 'b'], ['b', 'a'])).toBe(false);
    expect(sameEnv({ A: '1' }, { A: '1' })).toBe(true);
    expect(sameEnv({ A: '1' }, { A: '2' })).toBe(false);
    expect(sameEnv({ A: '1' }, { A: '1', B: '' })).toBe(false);
  });
});
