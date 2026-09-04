import { describe, expect, it } from 'vitest';
import type { ComposeService } from '../../types/compose';
import { layoutCompose } from './composeLayout';

const svc = (name: string): ComposeService => ({ name, image: `${name}:latest`, ports: [], environment: {}, dependsOn: [], networks: [], volumes: [], command: '', status: '' });

describe('layoutCompose', () => {
  it('places dependants one column after their deepest dependency', () => {
    const pos = layoutCompose([svc('web'), svc('db'), svc('cache')], [
      { source: 'db', target: 'web', type: 'depends_on', label: 'depends on' },
      { source: 'cache', target: 'web', type: 'depends_on', label: 'depends on' },
    ]);
    const by = Object.fromEntries(pos.map((p) => [p.name, p]));
    expect(by.db.depth).toBe(0);
    expect(by.cache.depth).toBe(0);
    expect(by.web.depth).toBe(1);
    expect(by.web.x).toBeGreaterThan(by.db.x);
    expect(by.db.y).not.toBe(by.cache.y);
  });
  it('survives cycles and unknown endpoints', () => {
    const pos = layoutCompose([svc('a'), svc('b')], [
      { source: 'a', target: 'b', type: 'network', label: 'net' },
      { source: 'b', target: 'a', type: 'network', label: 'net' },
      { source: 'zzz', target: 'a', type: 'network', label: 'net' },
    ]);
    expect(pos).toHaveLength(2);
    expect(pos.every((p) => Number.isFinite(p.x) && Number.isFinite(p.y))).toBe(true);
  });
});
