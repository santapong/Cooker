import { describe, expect, it } from 'vitest';
import { buildBreadcrumb } from './breadcrumb';

describe('buildBreadcrumb', () => {
  it('root is Apps', () => {
    expect(buildBreadcrumb('/')).toEqual([{ label: 'Apps' }]);
  });

  it('section pages are a single unlinked crumb', () => {
    expect(buildBreadcrumb('/pipelines')).toEqual([{ label: 'Pipelines' }]);
    expect(buildBreadcrumb('/settings')).toEqual([{ label: 'Settings' }]);
    expect(buildBreadcrumb('/admin/audit')).toEqual([{ label: 'Audit' }]);
  });

  it('nested nav items show their parent chain', () => {
    expect(buildBreadcrumb('/docker/compose')).toEqual([
      { label: 'Docker', to: '/docker' },
      { label: 'Compose' },
    ]);
  });

  it('known words become labels, ids render mono', () => {
    expect(buildBreadcrumb('/apps/new')).toEqual([
      { label: 'Apps', to: '/apps' },
      { label: 'New app' },
    ]);
    expect(buildBreadcrumb('/apps/a1b2')).toEqual([
      { label: 'Apps', to: '/apps' },
      { label: 'a1b2', mono: true },
    ]);
  });

  it('deployment detail links back through the app', () => {
    expect(buildBreadcrumb('/apps/a1/deployments/p9/r7')).toEqual([
      { label: 'Apps', to: '/apps' },
      { label: 'a1', to: '/apps/a1', mono: true },
      { label: 'Deployments', to: '/apps/a1/deployments' },
      { label: 'p9', to: '/apps/a1/deployments/p9', mono: true },
      { label: 'r7', mono: true },
    ]);
  });

  it('pipeline ids link to the editor (there is no bare /pipelines/:id route)', () => {
    expect(buildBreadcrumb('/pipelines/p1/edit')).toEqual([
      { label: 'Pipelines', to: '/pipelines' },
      { label: 'p1', to: '/pipelines/p1/edit', mono: true },
      { label: 'Edit' },
    ]);
    expect(buildBreadcrumb('/pipelines/p1/runs/r2')).toEqual([
      { label: 'Pipelines', to: '/pipelines' },
      { label: 'p1', to: '/pipelines/p1/edit', mono: true },
      { label: 'Runs', to: '/pipelines/p1/runs' },
      { label: 'r2', mono: true },
    ]);
  });

  it('falls back to capitalised segments for unknown paths', () => {
    expect(buildBreadcrumb('/nope/deep')).toEqual([
      { label: 'Nope', to: '/nope' },
      { label: 'Deep' },
    ]);
  });
});
