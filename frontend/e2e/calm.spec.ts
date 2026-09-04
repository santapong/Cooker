import { expect, test } from '@playwright/test';
import { gotoEditor, gotoRun, runningAnimations, willChangeViolations } from './fixtures/motion';

const AMBIENT = ['drift1', 'drift2', 'twinkle'];

test.describe('Calm mode — the WCAG 2.2.2 pause control', () => {
  test('stops ambient motion, persists, and restores it when switched off', async ({ page }) => {
    await gotoEditor(page);
    const before = (await runningAnimations(page)).map((a) => a.name);
    for (const n of AMBIENT) expect(before, 'ambient motion runs before Calm').toContain(n);

    const toggle = page.locator('.strip-toggle');
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-pressed', 'true');
    await expect.poll(() => page.evaluate(() => document.documentElement.dataset.calm)).toBe('true');
    await expect.poll(async () => (await runningAnimations(page)).filter((a) => AMBIENT.includes(a.name)).length).toBe(0);
    expect(await page.evaluate(() => JSON.parse(localStorage.getItem('cooker-ui') ?? '{}').state?.calmMode)).toBe(true);

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-pressed', 'false');
    await expect.poll(async () => (await runningAnimations(page)).some((a) => a.name === 'drift1')).toBe(true);
  });

  test('run view: Calm removes the comet and keeps the hot edge lit', async ({ page }) => {
    await gotoRun(page);
    await expect(page.locator('.comet')).toHaveCount(1);
    await page.locator('.strip-toggle').click();
    await expect(page.locator('.comet')).toHaveCount(0);
    await expect(page.locator('.constellation.is-hot')).toHaveCount(1);
    const halo = (await runningAnimations(page)).find((a) => a.target.includes('halo'));
    expect(halo?.name).toBe('halo-pulse-static');
  });
});

test.describe('compositor hygiene', () => {
  test('nothing inside #root pins will-change at idle (editor + run view)', async ({ page }) => {
    await gotoEditor(page);
    await page.waitForTimeout(1_000); // past the scene entrance
    expect(await willChangeViolations(page)).toEqual([]);
    await gotoRun(page);
    await page.waitForTimeout(1_000);
    expect(await willChangeViolations(page)).toEqual([]);
  });
});
