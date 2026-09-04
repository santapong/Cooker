import type { ComposeConnection, ComposeService } from '../../types/compose';

export interface ComposeNodePos {
  name: string;
  x: number;
  y: number;
  depth: number;
}

/**
 * Layered layout for a compose graph: a service's column is one past its
 * deepest dependency (depends_on / env_reference / network edges all count),
 * rows spread evenly within a column. Cycles fall back to depth 0.
 */
export function layoutCompose(services: ComposeService[], connections: ComposeConnection[], colGap = 260, rowGap = 120): ComposeNodePos[] {
  const names = services.map((s) => s.name);
  const deps = new Map<string, string[]>(names.map((n) => [n, []]));
  for (const c of connections) {
    if (deps.has(c.target) && deps.has(c.source) && c.source !== c.target) deps.get(c.target)!.push(c.source);
  }
  const depth = new Map<string, number>();
  const visiting = new Set<string>();
  const depthOf = (n: string): number => {
    if (depth.has(n)) return depth.get(n)!;
    if (visiting.has(n)) return 0;
    visiting.add(n);
    const d = Math.max(-1, ...(deps.get(n) ?? []).map(depthOf)) + 1;
    visiting.delete(n);
    depth.set(n, d);
    return d;
  };
  names.forEach(depthOf);
  const columns = new Map<number, string[]>();
  for (const n of names) {
    const d = depth.get(n) ?? 0;
    if (!columns.has(d)) columns.set(d, []);
    columns.get(d)!.push(n);
  }
  const tallest = Math.max(1, ...Array.from(columns.values()).map((c) => c.length));
  const out: ComposeNodePos[] = [];
  for (const [d, col] of columns) {
    const offset = ((tallest - col.length) * rowGap) / 2;
    col.forEach((n, i) => out.push({ name: n, depth: d, x: 80 + d * colGap, y: 80 + offset + i * rowGap }));
  }
  return out.sort((a, b) => names.indexOf(a.name) - names.indexOf(b.name));
}
