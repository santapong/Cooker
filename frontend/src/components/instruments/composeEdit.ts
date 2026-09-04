/** Text ⇄ structure helpers for the compose service editor (one entry per line). */

export function parseList(text: string): string[] {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean);
}

export function envToLines(env: Record<string, string> | undefined): string {
  return Object.entries(env ?? {})
    .map(([k, v]) => `${k}=${v}`)
    .join('\n');
}

const KEY = /^[A-Za-z_][A-Za-z0-9_.-]*$/;

/** `KEY=value` per line; blank lines are skipped. The first malformed line is reported. */
export function parseEnvLines(text: string): { env: Record<string, string> } | { error: string } {
  const env: Record<string, string> = {};
  const lines = text.split('\n');
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line) continue;
    const eq = line.indexOf('=');
    const key = eq < 0 ? line : line.slice(0, eq).trim();
    if (!KEY.test(key)) return { error: `Line ${i + 1}: "${key || line}" is not a valid variable name.` };
    env[key] = eq < 0 ? '' : line.slice(eq + 1);
  }
  return { env };
}

export function sameList(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i]);
}

export function sameEnv(a: Record<string, string>, b: Record<string, string>): boolean {
  const ka = Object.keys(a);
  const kb = Object.keys(b);
  return ka.length === kb.length && ka.every((k) => k in b && a[k] === b[k]);
}
