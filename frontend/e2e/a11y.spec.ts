import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { mockApi, mockCompose, mockSignedOut } from './fixtures/api';

const ROUTES: { path: string; ready: string; setup?: (page: Page) => Promise<void> }[] = [
  { path: '/pipelines', ready: 'a.chart-name' },
  { path: '/pipelines/gates/edit', ready: '.star' },
  { path: '/pipelines/gates/runs/r1', ready: '.star' },
  { path: '/docker/compose', ready: '.star', setup: mockCompose },
  { path: '/settings', ready: '.section' },
  { path: '/signin', ready: '.airlock-card form', setup: mockSignedOut },
  { path: '/signup', ready: '.airlock-card form', setup: mockSignedOut },
];

for (const { path, ready, setup } of ROUTES) {
  test(`axe: no serious or critical violations on ${path}`, async ({ page }, testInfo) => {
    await mockApi(page);
    if (setup) await setup(page);
    await page.goto(path);
    await expect(page.locator(ready).first()).toBeVisible();
    const results = await new AxeBuilder({ page }).analyze();
    const blocking = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical');
    const other = results.violations.filter((v) => v.impact !== 'serious' && v.impact !== 'critical');
    for (const v of other) testInfo.annotations.push({ type: `axe-${v.impact}`, description: `${v.id}: ${v.nodes.length} node(s) — ${v.help}` });
    expect(
      blocking.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`),
      'serious/critical axe violations',
    ).toEqual([]);
  });
}
