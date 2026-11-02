import { expect, test, type Page } from '@playwright/test';
import { mockApi } from './fixtures/api';
import type { AppDeployRecord } from '../src/types/app';

const rejected: AppDeployRecord = { id: 'rejected-attempt', appId: 'owned-app', runId: 'rejected-run', pipelineId: 'rejected-graph', status: 'failed', kind: 'rejected', createdAt: '2026-10-03T00:00:00Z' };

async function setup(page: Page, theme: string, failed = false) {
  await mockApi(page);
  await page.addInitScript((theme) => { localStorage.setItem('cooker.local.token', 'owned-history-fixture'); localStorage.setItem('cooker-ui', JSON.stringify({ state: { mode: 'pro', themeMode: theme, calmMode: true }, version: 0 })); }, theme);
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: 'admin', name: 'Owned administrator', role: 'admin', roles: ['admin'] } }));
  await page.route('**/api/v1/apps/owned-app', (route) => route.fulfill({ json: { id: 'owned-app', name: 'Owned app', githubRepo: 'owned/fixture', branch: 'main', deployTarget: { kind: 'docker-host' } } }));
  await page.route('**/api/v1/apps/owned-app/drift', (route) => route.fulfill({ json: { status: 'unsupported', checkedAt: rejected.createdAt } }));
  let fail = failed; let rows = [rejected]; let reads = 0;
  await page.route('**/api/v1/apps/owned-app/deploys?*', (route) => { reads++; return route.fulfill(fail ? { status: 503, json: { error: 'owned-private-history-error-canary' } } : { json: { deploys: rows } }); });
  const mutations: string[] = []; const errors: string[] = [];
  page.on('request', (request) => { const url = new URL(request.url()); if (url.pathname.startsWith('/api/v1/') && request.method() !== 'GET' && !url.pathname.includes('ws-tickets')) mutations.push(request.method() + ' ' + url.pathname); });
  page.on('pageerror', (error) => errors.push(error.message));
  return { fail: (value: boolean) => { fail = value; }, clear: () => { rows = []; }, reads: () => reads, mutations, errors };
}

test('a late failed refresh cannot replace a newer successful empty history', async ({ page }) => {
  const fixture = await setup(page, 'dark'); await page.goto('/apps/owned-app');
  await expect(page.getByText('Rejected before deployment', { exact: true })).toBeVisible();
  let count = 0; let release: (() => void) | undefined;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route('**/api/v1/apps/owned-app/deploys?*', async (route) => {
    count++;
    if (count === 1) { await held; return route.fulfill({ status: 503, json: { error: 'owned-private-history-error-canary' } }); }
    return route.fulfill({ json: { deploys: [] } });
  });
  const button = page.getByRole('button', { name: 'Refresh history', exact: true });
  await button.focus(); await button.press('Enter'); await expect.poll(() => count).toBe(1);
  await button.press('Enter'); await expect(page.getByText(/^No deploys yet/)).toBeVisible();
  const failedResponse = page.waitForResponse((response) => response.url().includes('/deploys?') && response.status() === 503);
  release?.(); await failedResponse;
  await expect(page.getByRole('alert').filter({ hasText: 'Deployment history is unavailable' })).toHaveCount(0);
  await expect(page.getByText(/^No deploys yet/)).toBeVisible();
  expect(fixture.mutations).toEqual([]); expect(fixture.errors).toEqual([]);
});

for (const theme of ['dark', 'light']) {
  test(`${theme} rejected attempt is readable and never a rollback target`, async ({ page }) => {
    await page.setViewportSize({ width: theme === 'dark' ? 1440 : 390, height: 900 });
    const fixture = await setup(page, theme); await page.goto('/apps/owned-app');
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    await expect(page.getByText('Rejected before deployment', { exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'porthole ↗', exact: true })).toHaveAttribute('href', '/apps/owned-app/deployments/rejected-graph/rejected-run');
    await expect(page.getByRole('button', { name: /^Roll back/ })).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText('No deploys yet');
    expect(fixture.mutations).toEqual([]); expect(fixture.errors).toEqual([]);
  });

  test(`${theme} failed history read is unavailable and retry can prove empty history`, async ({ page }) => {
    const fixture = await setup(page, theme, true); await page.goto('/apps/owned-app');
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    await expect(page.getByRole('alert').filter({ hasText: 'Deployment history is unavailable' })).toBeVisible();
    await expect(page.locator('body')).not.toContainText('No deploys yet');
    await expect(page.locator('body')).not.toContainText('owned-private-history-error-canary');
    fixture.fail(false); fixture.clear();
    await page.getByRole('button', { name: 'Refresh history', exact: true }).click();
    await expect(page.getByText(/^No deploys yet/)).toBeVisible();
    await expect(page.getByRole('alert').filter({ hasText: 'Deployment history is unavailable' })).toHaveCount(0);
    expect(fixture.reads()).toBeGreaterThan(1); expect(fixture.mutations).toEqual([]); expect(fixture.errors).toEqual([]);
  });

  test(`${theme} failed refresh retains explicitly stale attempts`, async ({ page }) => {
    const fixture = await setup(page, theme); await page.goto('/apps/owned-app');
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    await expect(page.getByText('Rejected before deployment', { exact: true })).toBeVisible();
    await page.route('**/api/v1/apps/owned-app', (route) => route.fulfill({ status: 503, json: { error: 'owned-private-app-error-canary' } }));
    fixture.fail(true); await page.getByRole('button', { name: 'Refresh history', exact: true }).click();
    await expect(page.getByRole('alert').filter({ hasText: 'Showing the last loaded attempts' })).toBeVisible();
    await expect(page.getByRole('alert').filter({ hasText: 'App details are unavailable' })).toBeVisible();
    await expect(page.getByText('Rejected before deployment', { exact: true })).toBeVisible();
    await expect(page.locator('body')).not.toContainText('owned-private-history-error-canary');
    await expect(page.locator('body')).not.toContainText('owned-private-app-error-canary');
    await page.route('**/api/v1/apps/owned-app', (route) => route.fulfill({ json: { id: 'owned-app', name: 'Owned app', githubRepo: 'owned/fixture', branch: 'main', deployTarget: { kind: 'docker-host' } } }));
    fixture.fail(false); await page.getByRole('button', { name: 'Refresh history', exact: true }).click();
    await expect(page.getByRole('alert').filter({ hasText: 'unavailable' })).toHaveCount(0);
    await expect(page.getByText('Rejected before deployment', { exact: true })).toBeVisible();
    expect(fixture.mutations).toEqual([]); expect(fixture.errors).toEqual([]);
  });
}
