import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { GATES_PIPELINE, GATES_RUN, mockApi } from './fixtures/api';

// Isolated presentation fixtures; these controls never send deployment actions.
for (const width of [320, 390, 768, 1440]) {
  for (const theme of ['dark', 'light'] as const) {
    for (const status of ['running', 'success'] as const) {
      test(`run controls remain readable at ${width}px in ${theme}, ${status}`, async ({ page }, testInfo) => {
        await page.setViewportSize({ width, height: 1000 });
        await page.emulateMedia({ reducedMotion: 'reduce' });
        await mockApi(page);
        await page.addInitScript(({ theme }) => {
          localStorage.setItem('cooker.local.token', 'owned-layout-session');
          localStorage.setItem('cooker-ui', JSON.stringify({ state: { mode: 'pro', themeMode: theme, calmMode: true }, version: 0 }));
        }, { theme });
        await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: 'owned-layout-admin', name: 'Layout fixture', email: 'layout@fixture.invalid', roles: ['admin'], role: 'admin' } }));
        const name = width === 1440 ? 'gates' : 'Cooker private image release';
        await page.route('**/api/v1/pipelines/gates', (route) => route.fulfill({ json: { ...GATES_PIPELINE, name } }));
        await page.route('**/api/v1/pipelines/gates/runs/r1', (route) => route.fulfill({ json: { ...GATES_RUN, status, finishedAt: status === 'success' ? '2026-09-05T00:00:04Z' : null } }));
        await page.route('**/api/v1/pipelines/gates/runs/r1/stage-approvals', (route) => route.fulfill({ json: { runId: 'r1', gates: [] } }));
        const writes: string[] = [];
        const errors: string[] = [];
        page.on('pageerror', (error) => errors.push(error.message));
        page.on('request', (request) => {
          const url = new URL(request.url());
          if (url.pathname.startsWith('/api/v1/') && request.method() !== 'GET' && !url.pathname.includes('ws-tickets')) writes.push(`${request.method()} ${url.pathname}`);
        });
        await page.goto('/pipelines/gates/runs/r1');
        const porthole = page.locator('.editor > .porthole');
        await expect(porthole.getByRole('heading', { level: 1 })).toContainText(name);
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        await expect(porthole.getByRole('button', { name: status === 'success' ? '▶ Re-run' : 'Cancel', exact: true })).toBeVisible();
        await expect(porthole.getByRole('button', { name: 'Promote', exact: true })).toBeVisible();
        const geometry = await porthole.evaluate((element) => {
          const box = (node: Element) => {
            const r = node.getBoundingClientRect();
            return { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
          };
          const heading = element.querySelector('.hud-title')!;
          const controls = [...element.querySelectorAll('.hud-tr .hud-stats, .hud-tr .badge, .hud-actions > *')];
          const boxes = [heading, ...controls].map(box);
          const overlaps: [number, number][] = [];
          for (let i = 0; i < boxes.length; i++) for (let j = i + 1; j < boxes.length; j++) {
            const a = boxes[i], b = boxes[j];
            if (Math.min(a.right, b.right) - Math.max(a.left, b.left) > 1 && Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 1) overlaps.push([i, j]);
          }
          const frame = box(element);
          const clipped = boxes.some((r) => r.left < frame.left || r.right > frame.right || r.top < frame.top || r.bottom > frame.bottom);
          return { overlaps, clipped, horizontalOverflow: document.querySelector('.shell-main')!.scrollWidth > document.querySelector('.shell-main')!.clientWidth + 1 };
        });
        await page.screenshot({ path: testInfo.outputPath('run-layout.png') });
        expect(geometry, 'Run title, status, elapsed label and actions must not overlap or leave the frame').toEqual({ overlaps: [], clipped: false, horizontalOverflow: false });
        await porthole.getByRole('button', { name: 'Promote', exact: true }).focus();
        await expect(porthole.getByRole('button', { name: 'Promote', exact: true })).toBeFocused();
        if (width === 390) {
          const report = await new AxeBuilder({ page }).include('.hud-tl').include('.hud-tr').analyze();
          expect(report.violations.filter((item) => ['critical', 'serious'].includes(item.impact ?? ''))).toEqual([]);
        }
        expect(writes).toEqual([]);
        expect(errors).toEqual([]);
      });
    }
  }
}
