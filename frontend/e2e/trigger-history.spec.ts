import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi } from './fixtures/api';
import type { TriggerHistoryRow, WebhookAttempt } from '../src/api/triggerHistory';

// Contract fixtures only. Rejected/unknown examples demonstrate presentation;
// they do not claim that pre-admission HTTP failures are currently persisted.
const historyStatuses = ['accepted', 'ignored', 'duplicate', 'stale', 'rejected', 'review_required', 'failed', 'archived', 'unknown'] as const;
const rows: TriggerHistoryRow[] = Array.from({ length: 34 }, (_, index) => ({
  deliveryId: `delivery-${String(index).padStart(2, '0')}`, providerDeliveryId: `provider-${index}`,
  provider: index % 2 ? 'gitlab' : 'github', connectionId: 'git-connection', repository: `team/shop-${index}`,
  fullRef: index % 2 ? 'refs/tags/v1' : 'refs/heads/main', commitSha: 'a'.repeat(40), deleted: false,
  appId: index === 33 ? undefined : 'history-app', ruleId: 'rule', ruleVersion: 2, targetSource: index === 33 ? undefined : 'intent',
  intentId: index === 33 ? undefined : `intent-${index}`, intentStatus: index % 9 === 5 ? 'awaiting_review' : 'queued',
  jobId: index === 0 ? 'job-0' : undefined, jobStatus: index === 0 ? 'succeeded' : undefined,
  pipelineId: index === 0 ? 'pipeline' : undefined, runId: index === 0 ? 'run-0' : undefined, runStatus: index === 0 ? 'failed' : undefined,
  status: index === 0 ? 'failed' : historyStatuses[index % historyStatuses.length],
  reasonCode: 'fixture', reason: 'This is a safe decision explanation. Private provider errors are omitted.',
  createdAt: '2026-10-02T00:00:00Z', updatedAt: '2026-10-02T01:00:00Z', archived: index % 9 === 7, decisionIndex: 1,
}));

async function historyFixture(page: Page, role = 'admin') {
  await mockApi(page);
  const requests: URL[] = []; const writes: string[] = [];
  await page.addInitScript(() => { localStorage.setItem('cooker.local.token', 'fixture-token'); localStorage.setItem('cooker-ui', JSON.stringify({ state: { themeMode: 'dark', calmMode: true }, version: 0 })); });
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: role, name: role, email: `${role}@fixture.invalid`, role, roles: [role] } }));
  await page.route('**/api/v1/apps/history-app', (route) => route.fulfill({ json: { id: 'history-app', name: 'Shop', githubRepo: 'team/shop', branch: 'main', version: 1, hasWebhook: false, autoDeploy: false, buildPlan: { kind: 'dockerfile' }, deployTarget: { kind: 'docker-host' }, createdAt: '2026-10-02T00:00:00Z', updatedAt: '2026-10-02T00:00:00Z' } }));
  await page.route('**/api/v1/apps/history-app/deploys', (route) => route.fulfill({ json: { deploys: [] } }));
  await page.route('**/api/v1/apps/history-app/drift', (route) => route.fulfill({ json: { status: 'unknown' } }));
  await page.route('**/api/v1/admin/trigger-rules', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/v1/admin/source-connections', (route) => route.fulfill({ json: [] }));
  let fail = false;
  const attempts: WebhookAttempt[] = Array.from({ length: 30 }, (_, i) => ({ id: `attempt-${i}`, provider: 'github', connectionId: 'git-connection', providerDeliveryId: i < 2 ? 'same-provider-delivery' : `provider-${i}`, deliveryId: i < 2 ? 'delivery-00' : undefined, status: i === 0 ? 'accepted' : i === 1 ? 'duplicate' : i === 2 ? 'received' : i === 3 ? 'uncertain' : 'rejected', reasonCode: i === 0 ? 'admitted' : i === 1 ? 'already_recorded' : i === 2 ? 'processing' : i === 3 ? 'admission_unconfirmed' : 'invalid_json', createdAt: '2026-10-02T00:00:00Z', finishedAt: i === 2 ? undefined : '2026-10-02T01:00:00Z' }));
  await page.route('**/api/v1/admin/webhook-attempts?*', (route) => {
    const q = new URL(route.request().url()).searchParams;
    const filtered = attempts.filter((a) => (!q.get('status') || a.status === q.get('status')) && (!q.get('appId') || !!a.deliveryId) && (!q.get('q') || `${a.id} ${a.providerDeliveryId}`.includes(q.get('q')!)));
    const offset = Number(q.get('offset')) || 0; const limit = Number(q.get('limit')) || 25;
    return route.fulfill({ json: { items: filtered.slice(offset, offset + limit), total: filtered.length, limit, offset, journalStartedAt: '2026-10-02T00:00:00Z', coverage: 'prospective_authenticated_connection_requests', limitations: ['Authentication failures are not journaled.', 'Earlier attempts cannot be reconstructed.'] } });
  });
  await page.route('**/api/v1/admin/trigger-deliveries**', (route) => {
    const u = new URL(route.request().url()); requests.push(u);
    if (fail) return route.fulfill({ status: 503, json: { error: 'Cannot read delivery history' } });
    const q = u.searchParams;
    const delivery = u.pathname.match(/\/trigger-deliveries\/([^/]+)\/history$/)?.[1];
    const filtered = rows.filter((r) => (!delivery || r.deliveryId === delivery) && (!q.get('appId') || r.appId === q.get('appId')) && (!q.get('provider') || r.provider === q.get('provider')) && (!q.get('status') || r.status === q.get('status')) && (!q.get('ref') || r.fullRef === q.get('ref')) && (!q.get('q') || `${r.repository} ${r.intentId} ${r.jobId}`.includes(q.get('q')!)));
    const limit = Number(q.get('limit')) || 25; const offset = Number(q.get('offset')) || 0;
    return route.fulfill({ json: { items: filtered.slice(offset, offset + limit), total: filtered.length, limit, offset, coverage: 'authenticated_admitted_decisions', limitations: ['Authentication failures rejected before inbox admission are not retained here.', 'Compacted ignored decisions may no longer be available.'] } });
  });
  page.on('request', (r) => { if (r.url().includes('/api/v1/') && r.method() !== 'GET' && !r.url().includes('ws-tickets')) writes.push(r.method() + ' ' + new URL(r.url()).pathname); });
  return { requests, writes, fail: (value: boolean) => { fail = value; } };
}

test('admin browses decision pages and filters before slicing with safe run/intent/job inspection', async ({ page }) => {
  const fixture = await historyFixture(page); await page.goto('/apps/history-app');
  const panel = page.getByRole('region', { name: 'Git delivery history' });
  await expect(panel.getByRole('status')).toContainText('1–25 of 33 decisions');
  await panel.getByRole('button', { name: 'Next decisions' }).click();
  await expect(panel.getByRole('status')).toContainText('26–33 of 33 decisions');
  expect(fixture.requests.at(-1)?.searchParams.get('offset')).toBe('25');
  await panel.getByLabel('Decision status').selectOption('failed');
  await expect(panel.getByRole('status')).toContainText('1–4 of 4 decisions');
  expect(fixture.requests.at(-1)?.searchParams.get('offset')).toBe('0');
  await panel.getByLabel('Search deliveries').fill('job-0');
  await expect(panel.getByRole('article')).toHaveCount(1);
  await panel.getByText('Inspect delivery, intent and job', { exact: true }).click();
  await expect(panel.getByText('Job job-0 · succeeded', { exact: true })).toBeVisible();
  await expect(panel.getByText('Run run-0 · failed', { exact: true })).toBeVisible();
  await expect(panel.getByRole('link', { name: 'Open linked run' })).toHaveAttribute('href', '/apps/history-app/deployments/pipeline/run-0');
  await panel.getByRole('link', { name: 'Inspect all delivery decisions' }).click();
  await expect(page).toHaveURL(/gitDelivery=delivery-00/);
  await expect(panel.getByRole('article')).toHaveCount(1); await page.reload();
  await expect(page.getByRole('region', { name: 'Git delivery history' }).getByRole('article')).toHaveCount(1);
  expect(fixture.writes).toEqual([]);
});

test('prospective request audit separates exact retries, rejected requests and unconfirmed outcomes from deployments', async ({ page }) => {
  const fixture = await historyFixture(page); await page.goto('/apps/history-app');
  const history = page.getByRole('region', { name: 'Git delivery history' });
  await history.getByText('Incoming webhook attempts', { exact: true }).click();
  const panel = page.getByRole('region', { name: 'Authenticated webhook attempts' });
  await expect(panel.getByRole('status')).toContainText('1–25 of 30 request attempts');
  await expect(panel.getByText(/Earlier attempts cannot be reconstructed/).first()).toBeVisible();
  await panel.getByLabel('Request outcome').selectOption('received');
  await expect(panel.getByRole('article')).toHaveCount(1);
  await expect(panel.getByText(/A crash or interrupted response is possible/)).toBeVisible();
  await expect(panel.getByText(/completion unconfirmed/)).toBeVisible();
  await panel.getByLabel('Request outcome').selectOption('uncertain'); await expect(panel.getByText(/Inbox admission could not be confirmed/)).toBeVisible();
  await panel.getByLabel('Request outcome').selectOption('rejected'); await expect(panel.getByRole('status')).toContainText('1–25 of 26 request attempts');
  await panel.getByRole('button', { name: 'Next request attempts' }).click(); await expect(panel.getByRole('status')).toContainText('26–26 of 26 request attempts');
  await panel.getByLabel('Request outcome').selectOption('duplicate'); await expect(panel.getByRole('status')).toContainText('1–1 of 1 request attempts');
  await expect(panel.getByText(/It did not re-admit a deployment intent/)).toBeVisible();
  await panel.getByText('Inspect request identities', { exact: true }).click();
  await expect(panel.getByText('Provider delivery same-provider-delivery', { exact: true })).toBeVisible();
  await panel.getByRole('link', { name: 'Open matching delivery decisions' }).click(); await expect(page).toHaveURL(/gitDelivery=delivery-00/);
  await expect(history.getByRole('article', { name: 'Failed delivery decision' })).toHaveCount(1);
  expect(fixture.writes).toEqual([]);
});

test('all-deliveries scope includes unassigned events and empty/failure states stay explicit and retryable', async ({ page }) => {
  const fixture = await historyFixture(page); await page.goto('/apps/history-app'); const panel = page.getByRole('region', { name: 'Git delivery history' });
  await panel.getByLabel('History scope').selectOption('all');
  await expect(panel.getByRole('status')).toContainText('1–25 of 34 decisions');
  await panel.getByLabel('Git provider').selectOption('gitlab');
  await panel.getByLabel('Exact source ref').fill('refs/tags/v1');
  await expect(panel.getByRole('status')).toContainText('1–17 of 17 decisions');
  await panel.getByLabel('Search deliveries').fill('missing'); await expect(panel.getByRole('status')).toHaveText('No matching decisions');
  await panel.getByText('What this history includes', { exact: true }).click();
  await expect(panel.getByText('Authentication failures rejected before inbox admission are not retained here.', { exact: true })).toBeVisible();
  fixture.fail(true); await panel.getByRole('button', { name: 'Refresh decisions' }).click(); await expect(panel.getByRole('alert')).toContainText('Delivery decisions could not be loaded. Check administrator access and refresh.');
  fixture.fail(false); await panel.getByRole('button', { name: 'Refresh decisions' }).click(); await expect(panel.getByRole('status')).toHaveText('No matching decisions');
  expect(fixture.writes).toEqual([]);
});

for (const role of ['viewer', 'operator']) test(`${role} retains existing admin-only automation boundaries`, async ({ page }) => {
  const fixture = await historyFixture(page, role); await page.goto('/apps/history-app');
  await expect(page.getByText('Ask an administrator to configure branch or release-tag deployment rules.', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Git delivery history' })).toHaveCount(0);
  expect(fixture.requests).toEqual([]); expect(fixture.writes).toEqual([]);
});

for (const theme of ['dark', 'light']) test(`delivery controls remain accessible in ${theme} and narrow layouts`, async ({ page }) => {
  await historyFixture(page);
  await page.addInitScript((themeMode) => localStorage.setItem('cooker-ui', JSON.stringify({ state: { themeMode, calmMode: true }, version: 0 })), theme);
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto('/apps/history-app');
  const panel = page.getByRole('region', { name: 'Git delivery history' }); await expect(panel.getByRole('status')).toContainText('33 decisions');
  await panel.getByLabel('Decision status').focus(); await page.keyboard.press('d'); await page.keyboard.press('Enter');
  await expect(panel.getByLabel('Decision status')).toBeFocused();
  await expect(panel.getByLabel('Decision status')).toHaveValue('duplicate');
  await panel.getByText('Incoming webhook attempts', { exact: true }).click();
  await expect(page.getByRole('region', { name: 'Authenticated webhook attempts' }).getByRole('status')).toContainText('30 request attempts');
  const results = await new AxeBuilder({ page }).include('section[aria-label="Git delivery history"]').withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
  expect(results.violations).toEqual([]);
});
