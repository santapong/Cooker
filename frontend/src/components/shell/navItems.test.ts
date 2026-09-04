import { describe, expect, it } from 'vitest';
import { activeNavId, NAV_GROUPS, NAV_ITEMS } from './navItems';

describe('NAV_ITEMS table', () => {
  it('has unique ids and every item belongs to a declared group', () => {
    const ids = NAV_ITEMS.map((i) => i.id);
    expect(new Set(ids).size).toBe(ids.length);
    const groups = new Set(NAV_GROUPS.map((g) => g.id));
    for (const item of NAV_ITEMS) expect(groups.has(item.group)).toBe(true);
  });

  it('lights itself for its own `to` route', () => {
    for (const item of NAV_ITEMS) expect(activeNavId(item.to)).toBe(item.id);
  });
});

describe('activeNavId', () => {
  it.each([
    ['/', 'apps'],
    ['/apps', 'apps'],
    ['/apps/new', 'apps'],
    ['/apps/a1b2', 'apps'],
    ['/apps/a1b2/deployments/p9/r7', 'apps'],
    ['/pipelines', 'pipelines'],
    ['/pipelines/p1/edit', 'pipelines'],
    ['/pipelines/p1/runs/r2', 'pipelines'],
    ['/docker', 'docker'],
    ['/docker/compose', 'compose'],
    ['/docker/compose/x', 'compose'],
    ['/kubernetes', 'kubernetes'],
    ['/cloud', 'cloud'],
    ['/environments', 'environments'],
    ['/hosts', 'hosts'],
    ['/registry', 'registry'],
    ['/admin/templates', 'templates'],
    ['/admin/schedules', 'schedules'],
    ['/admin/notifications', 'notifications'],
    ['/admin/audit', 'audit'],
    ['/analytics', 'analytics'],
    ['/settings', 'settings'],
  ])('%s → %s', (pathname, expected) => {
    expect(activeNavId(pathname)).toBe(expected);
  });

  it('returns null for routes the rail does not own', () => {
    expect(activeNavId('/signin')).toBeNull();
    expect(activeNavId('/callback')).toBeNull();
    expect(activeNavId('/admin')).toBeNull();
    expect(activeNavId('/nope/deep')).toBeNull();
  });
});
