import { expect, test } from '@playwright/test';
import { mockApi } from './fixtures/api';

const active = (page: Parameters<typeof mockApi>[0]) =>
  page.evaluate(() => `${document.activeElement?.tagName ?? ''}.${document.activeElement?.className ?? ''}`);

test.describe('spec §6.3 — focus after the list → porthole transition', () => {
  test('a star-chart row opens the porthole and focus lands on its heading', async ({ page }) => {
    await mockApi(page);
    await page.goto('/pipelines');
    await expect(page.locator('a.chart-name')).toHaveCount(1);
    await expect.poll(() => active(page)).toMatch(/^H1\./); // list heading takes focus on mount

    await page.locator('a.chart-name').first().click();
    await expect(page).toHaveURL(/\/pipelines\/gates\/edit$/);
    await expect.poll(() => active(page), { timeout: 5_000 }).toMatch(/^H1\..*hud-title/);
    await expect(page.locator('.star')).toHaveCount(3);
  });

  test('a direct deep link also lands focus on the porthole heading', async ({ page }) => {
    await mockApi(page);
    await page.goto('/pipelines/gates/runs/r1');
    await expect(page.locator('.star')).toHaveCount(3);
    await expect.poll(() => active(page)).toMatch(/^H1\..*hud-title/);
  });
});
