import { expect, test, type Page } from '@playwright/test';
import { mockApi } from './fixtures/api';

/** Local-only editor-readiness and layout-shift checks. The rectangular editor
 * has no artificial entrance animation; measure route-to-visible stage cards. */
test.skip(!!process.env.CI, 'local-only budget check');

interface Timing {
  clickToRoute: number;
  routeToFrame: number;
  frameToReady: number;
  routeToReady: number;
  clickToReady: number;
}

async function measureOpen(page: Page): Promise<Timing> {
  await page.goto('/pipelines');
  await expect(page.locator('a.chart-name')).toHaveCount(1);
  await page.evaluate(() => {
    const w = window as unknown as { __cls: number; __t0: number };
    w.__cls = 0;
    w.__t0 = performance.now();
  });
  await page.locator('a.chart-name').first().click();
  return page.evaluate(async () => {
    const w = window as unknown as { __t0: number };
    const start = performance.now();
    let routeAt = -1;
    while (performance.now() - start < 5_000) {
      if (routeAt < 0 && location.pathname.endsWith('/edit')) routeAt = performance.now();
      const el = document.querySelector('.pipeline-stage-card');
      if (el?.getBoundingClientRect().width) {
        const frameAt = performance.now();
        await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
        const doneAt = performance.now();
        return { clickToRoute: routeAt - w.__t0, routeToFrame: frameAt - routeAt, frameToReady: doneAt - frameAt, routeToReady: doneAt - routeAt, clickToReady: doneAt - w.__t0 };
      }
      await new Promise((r) => setTimeout(r, 5));
    }
    return { clickToRoute: -1, routeToFrame: -1, frameToReady: -1, routeToReady: -1, clickToReady: -1 };
  });
}

const round = (t: Timing) => Object.fromEntries(Object.entries(t).map(([k, v]) => [k, Math.round(v)]));

test('editor readiness timing (400 ms budget, strict on request) and CLS < 0.02', async ({ page }, testInfo) => {
  await mockApi(page);
  await page.addInitScript(() => {
    const w = window as unknown as { __cls: number };
    w.__cls = 0;
    new PerformanceObserver((list) => {
      for (const e of list.getEntries() as (PerformanceEntry & { value: number; hadRecentInput: boolean })[]) {
        if (!e.hadRecentInput) w.__cls += e.value;
      }
    }).observe({ type: 'layout-shift', buffered: true });
  });
  // warm the lazy editor chunk
  await page.goto('/pipelines/gates/edit');
  await expect(page.locator('.pipeline-stage-card')).toHaveCount(3);

  const timing = await measureOpen(page);
  await expect(page.locator('.pipeline-stage-card')).toHaveCount(3);
  await page.waitForTimeout(700);
  const cls = await page.evaluate(() => (window as unknown as { __cls: number }).__cls);

  console.log(`editor=${JSON.stringify(round(timing))} cls=${cls.toFixed(4)}`);
  testInfo.annotations.push({ type: 'editor-ready-ms (route → ready)', description: String(Math.round(timing.routeToReady)) });
  testInfo.annotations.push({ type: 'editor-ready-ms (click → ready)', description: String(Math.round(timing.clickToReady)) });
  testInfo.annotations.push({ type: 'cls', description: cls.toFixed(4) });

  expect(timing.routeToReady).toBeGreaterThan(0);
  expect(cls).toBeLessThan(0.02);
  // Enforce the 400ms target only on an explicitly requested quiet-machine run.
  // Headless rendering load otherwise makes these local timings observational.
  const budget = Number(process.env.PORTHOLE_OPEN_BUDGET_MS ?? 400);
  if (process.env.PERF_STRICT) expect(timing.routeToReady, `editor readiness budget ${budget} ms`).toBeLessThanOrEqual(budget);
  else testInfo.annotations.push({ type: 'budget', description: `${budget} ms not enforced (set PERF_STRICT=1)` });
});
