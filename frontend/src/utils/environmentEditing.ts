import { ApiError } from '../api/client';
import type { Environment } from '../types/environment';

export const environmentKeyPattern = /^[A-Za-z_][A-Za-z0-9_]{0,255}$/;
export function ordinaryValues(environment?: Environment): Record<string, string> {
  return environment?.plainVars ?? environment?.variables ?? {};
}
export function knownEnvironmentKeys(environment?: Environment): string[] {
  return [...new Set([...Object.keys(ordinaryValues(environment)), ...(environment?.secretKeys ?? [])])].sort();
}
export function configurationText(environment?: Environment): string {
  const entries = Object.entries(ordinaryValues(environment)).sort(([a], [b]) => a.localeCompare(b));
  return entries.some(([, value]) => /[\r\n]/.test(value)) ? JSON.stringify(Object.fromEntries(entries), null, 2) : entries.map(([key, value]) => `${key}=${value}`).join('\n');
}
/** Values after '=' remain exact. Errors never echo a configuration value. */
export function parseConfiguration(text: string, protectedKeys: readonly string[] = []): Record<string, string> {
  if (new TextEncoder().encode(text).length > 65536) throw new Error('Configuration must fit within 64 KiB.');
  const values: Record<string, string> = Object.create(null);
  if (text.trimStart().startsWith('{')) {
    let parsed: unknown;
    try { parsed = JSON.parse(text); } catch { throw new Error('Use a JSON object with string values for multiline configuration.'); }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || !Object.values(parsed).every((value) => typeof value === 'string')) throw new Error('Readable configuration JSON must contain string values only.');
    const seen = new Set<string>();
    // JSON strings followed by ':' are property names. Nested containers have
    // already been rejected above, including escaped aliases of duplicate keys.
    for (const match of text.matchAll(/("(?:\\.|[^"\\])*")\s*:/g)) {
      const key = JSON.parse(match[1]) as string;
      if (seen.has(key)) throw new Error('Each configuration key must appear once.');
      seen.add(key);
    }
    for (const [key, value] of Object.entries(parsed)) {
      if (!environmentKeyPattern.test(key)) throw new Error('Use valid environment key names.');
      if (protectedKeys.includes(key)) throw new Error('A protected key cannot also be readable configuration. Remove or rename that key first.');
      values[key] = value as string;
    }
    return values;
  }
  for (const line of text.split('\n')) {
    if (!line.trim()) continue;
    const separator = line.indexOf('=');
    const key = line.slice(0, separator).trim();
    if (separator < 1 || !environmentKeyPattern.test(key)) throw new Error('Use one valid KEY=value per line.');
    if (Object.prototype.hasOwnProperty.call(values, key)) throw new Error('Each configuration key must appear once.');
    if (protectedKeys.includes(key)) throw new Error('A protected key cannot also be readable configuration. Remove or rename that key first.');
    values[key] = line.slice(separator + 1);
  }
  return values;
}
export function configurationError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 409) return 'The saved version changed. Your draft is retained. Refresh and compare it before retrying.';
    if (error.status === 403) return 'Your permissions or MFA must be verified before saving. Your draft is retained.';
    if (error.status === 404) return 'This saved record is unavailable. Your draft is retained.';
    if (error.status === 501) return 'This backend does not support version-checked protected editing. Your draft is retained; no unversioned retry will be sent.';
    if (error.status === 400) return 'The server rejected this configuration. Review the keys and bindings. Your draft is retained.';
  }
  return 'The save could not be confirmed. Your draft is retained; refresh saved data before retrying.';
}

export function toggleEnvironmentKey(keys: readonly string[], key: string, selected: boolean): string[] {
  return selected ? [...new Set([...keys, key])].sort() : keys.filter((candidate) => candidate !== key);
}
