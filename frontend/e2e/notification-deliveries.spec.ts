import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi } from './fixtures/api';
import type { NotificationDelivery } from '../src/api/notificationDeliveries';

// Owned presentation fixtures only. No notification recipient is contacted.
const states = ['needs_attention', 'retry_wait', 'delivered', 'failed', 'cancelled', 'sending', 'pending', 'future_state'];
const rows: NotificationDelivery[] = Array.from({ length: 31 }, (_, index) => ({
  id: `delivery-${index}`, eventId: `event-${index}`, eventType: index % 2 ? 'run.succeeded' : 'deploy.failed',
  targetId: index % 2 ? 'target-two' : 'target-one', targetVersion: 3, kind: 'webhook',
  state: states[index % states.length], attempts: 1, maxAttempts: 3,
  nextAttemptAt: '2026-10-02T08:00:30Z', lastCode: index === 0 ? 'send_outcome_unknown' : undefined,
  lastHttpStatus: index === 1 ? 503 : undefined, duplicatePossible: index === 0,
  createdAt: '2026-10-02T08:00:00Z', updatedAt: '2026-10-02T08:00:00Z',
}));

async function setup(page: Page, role = 'admin') {
  await mockApi(page);
  await page.addInitScript(() => localStorage.setItem('cooker.local.token', 'owned-fixture-token'));
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: role, name: role, email: `${role}@fixture.invalid`, role, roles: [role] } }));
  await page.route('**/api/v1/admin/source-connections', (route) => route.fulfill({ json: [] }));
  const reads: URL[] = []; const writes: string[] = [];
  let fail = 0; let repairJobs = 0;
  await page.route('**/api/v1/admin/notification-deliveries**', (route) => {
    const request = route.request();
    if (request.method() !== 'GET') { writes.push(request.method()); return route.fulfill({ status: 405 }); }
    const url = new URL(request.url()); reads.push(url);
    if (fail) return route.fulfill({ status: fail, json: { error: 'synthetic-secret-canary-in-error-must-not-render' } });
    const q = url.searchParams;
    const filtered = rows.filter((row) => (!q.get('state') || row.state === q.get('state')) && (!q.get('targetId') || row.targetId === q.get('targetId')) && (!q.get('runId') || (q.get('runId') === 'run-one' && row.targetId === 'target-one')));
    const offset = Number(q.get('offset')); const limit = Number(q.get('limit'));
    return route.fulfill({ json: { items: filtered.slice(offset, offset + limit), total: filtered.length, mode: 'Owned fixture', pendingSourceJobs: 2, pendingSourceRepairJobs: repairJobs } });
  });
  return { reads, writes, fail: (status: number) => { fail = status; }, repairs: (n: number) => { repairJobs = n; } };
}

test('explains uncertainty and retry policy, server pages and filters before pagination', async ({ page }) => {
  const fixture = await setup(page); await page.goto('/settings');
  const panel = page.getByRole('region', { name: 'Notification delivery history' });
  await expect(panel.getByText('1–25 of 31 deliveries', { exact: true })).toBeVisible();
  await expect(panel.getByRole('article')).toHaveCount(25);
  await expect(panel.getByRole('article', { name: 'Needs attention notification', exact: true }).first()).toContainText('will not automatically resend');
  await expect(panel.getByRole('article', { name: 'Unknown delivery state notification', exact: true })).toHaveCount(3);
  await panel.getByText('How retries and recovery work', { exact: true }).click();
  await expect(panel.getByText(/Transient HTTP rejection can retry/)).toBeVisible();
  await panel.getByRole('button', { name: 'Next delivery page', exact: true }).click();
  await expect(panel.getByText('26–31 of 31 deliveries', { exact: true })).toBeVisible();
  await panel.getByLabel(/^Notification delivery state/).selectOption('needs_attention');
  await panel.getByLabel(/^Notification target ID/).fill('target-one');
  await panel.getByRole('button', { name: 'Apply delivery filters', exact: true }).click();
  await expect(panel.getByRole('article')).toHaveCount(4);
  const last = fixture.reads.at(-1)!.searchParams;
  expect(last.get('state')).toBe('needs_attention'); expect(last.get('targetId')).toBe('target-one'); expect(last.get('offset')).toBe('0');
  expect(fixture.writes).toEqual([]);
});

test('empty and error states do not claim complete delivery, and refresh recovers without sends', async ({ page }) => {
  const fixture = await setup(page); fixture.repairs(3); await page.goto('/settings');
  const panel = page.getByRole('region', { name: 'Notification delivery history' });
  await expect(panel.getByText(/Some source events need repair/)).toBeVisible();
  await panel.getByLabel(/^Notification run ID/).fill('missing-run');
  await panel.getByRole('button', { name: 'Apply delivery filters', exact: true }).click();
  await expect(panel.getByText('No matching notification deliveries.', { exact: true })).toBeVisible();
  fixture.fail(503); await panel.getByRole('button', { name: 'Refresh deliveries', exact: true }).click();
  await expect(panel.getByRole('alert')).toContainText('Could not read durable notification history');
  await expect(panel.getByText('No matching notification deliveries.', { exact: true })).toHaveCount(0);
  await expect(panel).not.toContainText('synthetic-secret-canary');
  fixture.fail(0); await panel.getByRole('button', { name: 'Refresh deliveries', exact: true }).click();
  await expect(panel.getByText('No matching notification deliveries.', { exact: true })).toBeVisible();
  expect(fixture.writes).toEqual([]);
});

test('viewer and operator cannot request administrative delivery history', async ({ page }) => {
  for (const role of ['viewer', 'operator']) {
    const fixture = await setup(page, role); await page.goto('/settings');
    await expect(page.getByText('Notification delivery history is available to administrators.', { exact: true })).toBeVisible();
    expect(fixture.reads).toHaveLength(0); expect(fixture.writes).toEqual([]);
  }
});

for (const theme of ['dark', 'light']) {
  test(`${theme} notification history supports narrow layout, keyboard and accessibility`, async ({ page }) => {
    const fixture = await setup(page);
    await page.addInitScript((theme) => localStorage.setItem('cooker-ui', JSON.stringify({ state: { themeMode: theme, calmMode: true }, version: 0 })), theme);
    await page.setViewportSize({ width: 390, height: 844 }); await page.goto('/settings');
    const panel = page.getByRole('region', { name: 'Notification delivery history' });
    await expect(panel.getByRole('article')).toHaveCount(25);
    await panel.getByRole('button', { name: 'Refresh deliveries', exact: true }).focus();
    await page.keyboard.press('Enter'); await expect(panel.getByRole('article')).toHaveCount(25);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    const result = await new AxeBuilder({ page }).include('.notification-history').analyze();
    expect(result.violations.filter((v) => v.impact === 'critical' || v.impact === 'serious')).toEqual([]);
    expect(fixture.writes).toEqual([]);
  });
}
