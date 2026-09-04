import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { mockApi } from './fixtures/api';

const ROUTES: { path: string; ready: string }[] = [
  { path: '/pipelines', ready: 'a.chart-name' },
  { path: '/pipelines/gates/edit', ready: '.star' },
  { path: '/pipelines/gates/runs/r1', ready: '.star' },
  { path: '/settings', ready: '.section' },
];

for (const { path, ready } of ROUTES) {
  test(`axe: no serious or critical violations on ${path}`, async ({ page }, testInfo) => {
    await mockApi(page);
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
