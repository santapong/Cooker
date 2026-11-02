import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { readFile } from 'node:fs/promises';
import { mockApi } from './fixtures/api';
import type { CatalogService } from '../src/types/catalog';
import type { RuntimeLogPage } from '../src/types/runtimeLogs';

// Presentation/auth-role fixtures only. Actual Docker/official HTTP evidence is
// recorded separately; no cloud or owner's session is accessed by these tests.
const ids = ['a'.repeat(64), 'b'.repeat(64)];
const services: CatalogService[] = [
  { id: 'native-service', appId: 'owned', appName: 'Owned log app', appHref: '/apps/owned', name: 'web', nativeServiceId: 'web', provider: 'docker-host', management: 'cooker', environment: 'team-test', status: 'success', health: 'unknown', definition: 'recorded', deploymentId: 'deployment', deploymentHref: '/apps/owned/deployments/pipeline/run' },
  { id: 'imported-service', appId: 'vapp_fixture', appName: 'Imported site', appHref: '/apps/synced/vapp_fixture', name: 'Website', provider: 'vercel', management: 'vercel', environment: 'production', status: 'READY', health: 'unknown', definition: 'deployed', deploymentId: 'vercel-deployment', inspectorUrl: 'https://vercel.com/fixture/site/deployment' },
];
function snapshot(offset = 0): RuntimeLogPage {
  const entries = Array.from({ length: offset === 0 ? 200 : 5 }, (_, index) => ({ sequence: offset + index + 1, timestamp: '2026-10-03T03:00:00Z', message: index === 0 && offset === 0 ? 'secret [REDACTED] <img src=x onerror=alert(1)>' : `marker ${offset + index + 1}` }));
  return { sessionId: 'snapshot-one', appId: 'owned', service: 'web', runId: 'accepted-run', containerId: ids[1], since: '2026-10-03T02:00:00Z', until: '2026-10-03T03:00:00Z', expiresAt: new Date(Date.now() + 600000).toISOString(), offset, nextOffset: offset === 0 ? 200 : undefined, total: 205, tailLimitReached: true, coverage: 'Bounded Docker tail in capture order; older or driver-expired logs may be unavailable.', entries };
}
async function setup(page: Page, role = 'operator', theme = 'dark') {
  await mockApi(page);
  await page.addInitScript(({ theme }) => {
    localStorage.setItem('cooker.local.token', 'owned-log-layout-session');
    localStorage.setItem('cooker-ui', JSON.stringify({ state: { mode: 'pro', themeMode: theme, calmMode: true }, version: 0 }));
  }, { theme });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: 'owned-log-user', email: 'logs@fixture.invalid', name: 'Log fixture', roles: [role], role } }));
  await page.route('**/api/v1/catalog/services?*', (route) => route.fulfill({ json: { items: services, total: 2, limit: 50, offset: 0, sources: [{ provider: 'cooker', available: true }, { provider: 'vercel', available: true }] } }));
  await page.route('**/api/v1/apps/owned/services/web/log-replicas', (route) => route.fulfill({ json: { appId: 'owned', service: 'web', runId: 'accepted-run', replicas: ids.map((containerId, index) => ({ containerId, name: `owned-web-${index + 1}`, state: 'running' })) } }));
  const requests: { method: string; path: string; body: unknown }[] = [];
  await page.route('**/api/v1/apps/owned/services/web/log-sessions**', async (route) => {
    const request = route.request(); const url = new URL(request.url());
    requests.push({ method: request.method(), path: url.pathname + url.search, body: request.postDataJSON() });
    if (request.method() === 'DELETE') return route.fulfill({ status: 204 });
    if (request.method() === 'POST') return route.fulfill({ status: 201, json: snapshot() });
    if (url.pathname.endsWith('/download')) return route.fulfill({ contentType: 'text/plain', body: 'secret [REDACTED]\nmarker 205\n' });
    return route.fulfill({ json: snapshot(Number(url.searchParams.get('offset')) || 0) });
  });
  await page.goto('/services');
  await page.getByRole('button', { name: 'Browse current Docker logs' }).click();
  await expect(page.getByRole('combobox', { name: 'Replica', exact: true })).toBeVisible();
  return requests;
}
async function capture(page: Page) {
  await page.getByRole('combobox', { name: 'Replica', exact: true }).selectOption(ids[1]);
  await page.getByRole('button', { name: 'Capture snapshot', exact: true }).click();
  await expect(page.getByLabel('web captured log output')).toContainText('[REDACTED]');
}

for (const width of [320, 390, 768, 1440]) for (const theme of ['dark', 'light']) {
  test(`fixed snapshot, paging, keyboard and layout at ${width}px in ${theme}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1100 });
    const errors: string[] = []; page.on('pageerror', (error) => errors.push(error.message));
    const requests = await setup(page, 'operator', theme);
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    expect(await page.getByRole('combobox', { name: 'Replica', exact: true }).inputValue()).toBe('');
    await expect(page.getByRole('button', { name: 'Capture snapshot', exact: true })).toBeDisabled();
    await page.getByRole('combobox', { name: 'Replica', exact: true }).focus();
    await expect(page.getByRole('combobox', { name: 'Replica', exact: true })).toBeFocused();
    await page.getByRole('combobox', { name: 'Time range', exact: true }).selectOption('15m');
    await page.getByLabel('Search redacted text', { exact: true }).fill('[REDACTED]');
    await capture(page);
    const request = requests.find((r) => r.method === 'POST')!;
    expect(request.body).toMatchObject({ containerId: ids[1], expectedRunId: 'accepted-run', search: '[REDACTED]', maxLines: 2000 });
    await expect(page.getByText('The tail limit was reached.', { exact: false })).toBeVisible();
    const output = page.getByLabel('web captured log output');
    await expect(output.locator('img')).toHaveCount(0);
    await page.getByText('Captured deployment and container', { exact: true }).click();
    await expect(page.getByText(ids[1], { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Next lines', exact: true }).focus();
    await page.keyboard.press('Enter');
    await expect(output).toContainText('marker 205');
    await expect(page.getByRole('button', { name: 'Next lines', exact: true })).toBeDisabled();
    await page.getByRole('button', { name: 'Previous lines', exact: true }).click();
    await expect(output).toContainText('[REDACTED]');
    const downloaded = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download redacted logs', exact: true }).focus();
    await page.keyboard.press('Space');
    const download = await downloaded;
    expect(download.suggestedFilename()).toBe('cooker-service-logs.txt');
    const path = await download.path();
    expect(await readFile(path!, 'utf8')).toBe('secret [REDACTED]\nmarker 205\n');
    await expect(page.getByRole('button', { name: 'Browse current Docker logs' })).toHaveCount(0);
    expect(await page.locator('.shell-main').evaluate((element) => element.scrollWidth > element.clientWidth + 1)).toBe(false);
    expect(await page.locator('.service-log-browser').evaluate((element) => {
      const r = element.getBoundingClientRect();
      return [...element.querySelectorAll('button,input,select')].some((node) => { const box = node.getBoundingClientRect(); return box.left < r.left - 1 || box.right > r.right + 1; });
    })).toBe(false);
    if (width === 390) {
      const report = await new AxeBuilder({ page }).include('.service-log-inspector').analyze();
      expect(report.violations.filter((v) => ['critical', 'serious'].includes(v.impact ?? ''))).toEqual([]);
    }
    await page.screenshot({ path: testInfo.outputPath('service-logs.png') });
    await page.getByRole('button', { name: 'Close log browser', exact: true }).click();
    await expect.poll(() => requests.some((r) => r.method === 'DELETE')).toBe(true);
    expect(requests.every((r) => r.path.startsWith('/api/v1/apps/owned/services/web/log-sessions'))).toBe(true);
    expect(errors).toEqual([]);
  });
}

test('viewer and imported projects never acquire native log controls', async ({ page }) => {
  await mockApi(page);
  await page.addInitScript(() => localStorage.setItem('cooker.local.token', 'owned-viewer-session'));
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: 'viewer', email: 'viewer@fixture.invalid', roles: ['viewer'], role: 'viewer' } }));
  await page.route('**/api/v1/catalog/services?*', (route) => route.fulfill({ json: { items: services, total: 2, sources: [] } }));
  const requests: string[] = []; page.on('request', (r) => { if (r.url().includes('/log-')) requests.push(r.url()); });
  await page.goto('/services');
  await expect(page.getByText('Runtime logs require an operator or administrator role.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Browse current Docker logs' })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Inspect / logs in Vercel ↗' })).toHaveAttribute('href', 'https://vercel.com/fixture/site/deployment');
  expect(requests).toEqual([]);
});

test('transient page failure retries the same immutable cursor; expiry removes output', async ({ page }) => {
  const requests = await setup(page);
  await capture(page);
  let attempt = 0;
  await page.route('**/log-sessions/snapshot-one?offset=200&limit=200', (route) => {
    attempt++;
    return attempt === 1 ? route.fulfill({ status: 500, json: { error: 'private-error-canary' } }) : route.fulfill({ json: snapshot(200) });
  });
  await page.getByRole('button', { name: 'Next lines', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Retry the same snapshot');
  await expect(page.getByLabel('web captured log output')).toContainText('[REDACTED]');
  await page.getByRole('button', { name: 'Next lines', exact: true }).click();
  await expect(page.getByLabel('web captured log output')).toContainText('marker 205');
  expect(attempt).toBe(2);
  expect(requests.filter((r) => r.method === 'POST')).toHaveLength(1);
  await page.route('**/log-sessions/snapshot-one?offset=0&limit=200', (route) => route.fulfill({ status: 404, json: { error: 'private-expiry-canary' } }));
  await page.getByRole('button', { name: 'Previous lines', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('expired');
  await expect(page.getByLabel('web captured log output')).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('private-');
});

test('late capture after close is discarded and its session released', async ({ page }) => {
  const requests = await setup(page);
  let release!: () => void;
  let arrived = false;
  const waiting = new Promise<void>((resolve) => { release = resolve; });
  await page.route('**/apps/owned/services/web/log-sessions', async (route) => { arrived = true; await waiting; return route.fulfill({ status: 201, json: snapshot() }); });
  await page.getByRole('combobox', { name: 'Replica', exact: true }).selectOption(ids[1]);
  await page.getByRole('button', { name: 'Capture snapshot', exact: true }).click();
  await expect.poll(() => arrived).toBe(true);
  await page.getByRole('button', { name: 'Close log browser', exact: true }).click();
  release();
  await expect.poll(() => requests.some((r) => r.method === 'DELETE')).toBe(true);
  await expect(page.getByLabel('web captured log output')).toHaveCount(0);
});

test('custom ranges over 24 hours and mismatched capture identities display no output', async ({ page }) => {
  const requests = await setup(page);
  await page.getByRole('combobox', { name: 'Replica', exact: true }).selectOption(ids[1]);
  await page.getByRole('combobox', { name: 'Time range', exact: true }).selectOption('custom');
  await page.getByLabel('From (local time)', { exact: true }).fill('2026-10-01T00:00');
  await page.getByLabel('Until (local time)', { exact: true }).fill('2026-10-03T00:00');
  await page.getByRole('button', { name: 'Capture snapshot', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('24 hours');
  expect(requests.filter((r) => r.method === 'POST')).toHaveLength(0);
  await page.getByRole('combobox', { name: 'Time range', exact: true }).selectOption('1h');
  await page.route('**/apps/owned/services/web/log-sessions', (route) => route.fulfill({ status: 201, json: { ...snapshot(), containerId: ids[0] } }));
  await page.getByRole('button', { name: 'Capture snapshot', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('could not be read');
  await expect(page.getByLabel('web captured log output')).toHaveCount(0);
});


test('local snapshot expiry clears rendered output without another capture', async ({ page }) => {
  await page.clock.install();
  const requests = await setup(page);
  await capture(page);
  await page.clock.fastForward(600001);
  await expect(page.getByRole('alert')).toContainText('This snapshot expired');
  await expect(page.getByLabel('web captured log output')).toHaveCount(0);
  expect(requests.filter((r) => r.method === 'POST')).toHaveLength(1);
});

// Exercise the real deployment RunView/inspector and ticketed WebSocket hooks.
// The routes and log frames are fixtures; these are not actual runtime proofs.
async function recordedRuntime(page: Page, options: { compose?: boolean; theme?: string; runtime?: string; appContext?: boolean } = {}) {
  await mockApi(page);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.addInitScript((theme) => {
    localStorage.setItem('cooker.local.token', 'runtime-controls-fixture');
    localStorage.setItem('cooker-ui', JSON.stringify({ state: { themeMode: theme, mode: 'pro', calmMode: true }, version: 0 }));
  }, options.theme ?? 'dark');
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: 'runtime-operator', email: 'runtime@fixture.invalid', role: 'operator', roles: ['operator'] } }));
  const service = options.compose ? 'front-web' : 'deploy';
  const config = { runtimeName: 'recorded-fixture-container', deployRuntime: options.runtime ?? 'docker', ...(options.compose ? { composeServiceName: service } : {}) };
  const stages = [
    { id: 'deploy', name: options.compose ? 'Compose website' : 'Dockerfile website', type: 'deploy', config, position: { x: 120, y: 100 } },
    { id: 'build', name: 'Build image', type: 'build', config: { ...config, composeServiceName: 'not-a-runtime' }, position: { x: 120, y: 270 } },
    { id: 'approval', name: 'Release review', type: 'approval', config: { ...config, composeServiceName: 'not-a-runtime' }, position: { x: 430, y: 100 } },
    { id: 'unrecorded', name: 'Unrecorded deployment', type: 'deploy', config: { deployRuntime: 'docker', composeServiceName: 'not-a-runtime' }, position: { x: 430, y: 270 } },
    { id: 'unsupported', name: 'Unsupported runtime', type: 'deploy', config: { ...config, deployRuntime: 'unsupported', composeServiceName: 'not-a-runtime' }, position: { x: 740, y: 100 } },
  ];
  await page.route('**/api/v1/apps/runtime-owned', (route) => route.fulfill({ json: { id: 'runtime-owned', name: 'Runtime fixture', buildPlan: { kind: options.compose ? 'compose' : 'dockerfile' }, deployTarget: { kind: 'docker-host' }, healthStatus: 'unknown' } }));
  await page.route('**/api/v1/pipelines/runtime-fixture', (route) => route.fulfill({ json: { id: 'runtime-fixture', name: 'Runtime fixture', stages, edges: [], variables: {}, version: 1 } }));
  await page.route('**/api/v1/pipelines/runtime-fixture/runs/recorded-run', (route) => route.fulfill({ json: { id: 'recorded-run', pipelineId: 'runtime-fixture', status: 'success', stageRuns: stages.map((stage) => ({ stageId: stage.id, status: 'success' })), variables: {}, environmentStatuses: [], pipelineVersion: 1 } }));
  await page.route('**/api/v1/pipelines/runtime-fixture/runs/recorded-run/stage-approvals', (route) => route.fulfill({ json: { runId: 'recorded-run', gates: [{ id: 'gate', runId: 'recorded-run', stageId: 'approval', status: 'approved', requiredApprovers: 1 }] } }));
  await page.route('**/api/v1/pipelines/runtime-fixture/runs/recorded-run/logs/*', (route) => route.fulfill({ json: { logs: 'recorded stage output' } }));
  const statusRequests: string[] = [], tickets: string[] = [], sockets: string[] = [], closed: string[] = [];
  await page.route('**/api/v1/apps/runtime-owned/services/*/runtime', (route) => {
    statusRequests.push(new URL(route.request().url()).pathname);
    return route.fulfill({ json: { runtime: options.runtime ?? 'docker', ref: config.runtimeName, state: 'running', healthy: false, message: 'Fixture readiness has not been checked' } });
  });
  await page.route('**/api/v1/ws-tickets', (route) => {
    const path = route.request().postDataJSON().path as string;
    if (!path.startsWith('/ws/runtime/')) return route.fulfill({ status: 403, json: { error: 'fixture stage stream disabled' } });
    tickets.push(path);
    return route.fulfill({ json: { ticket: 'runtime-fixture-ticket' } });
  });
  await page.routeWebSocket('**/ws/runtime/**', (ws) => {
    const path = new URL(ws.url()).pathname;
    sockets.push(path);
    ws.onClose(() => { closed.push(path); });
    ws.send('current recorded service log');
  });
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto(options.appContext === false ? '/pipelines/runtime-fixture/runs/recorded-run' : '/apps/runtime-owned/deployments/runtime-fixture/recorded-run');
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Runtime fixture');
  await expect(page.locator('html')).toHaveAttribute('data-theme', options.theme ?? 'dark');
  return { service, statusRequests, tickets, sockets, closed, errors };
}

for (const compose of [false, true]) for (const theme of ['dark', 'light']) {
  test(`recorded runtime ${compose ? 'Compose name' : 'single-image stage ID'} controls, status and logs in ${theme}`, async ({ page }, testInfo) => {
    const state = await recordedRuntime(page, { compose, theme });
    await page.locator('.react-flow__node[data-id="deploy"]').click();
    const inspector = page.getByRole('complementary', { name: `Stage ${compose ? 'Compose website' : 'Dockerfile website'}` });
    await expect(inspector).toContainText(`Runtime · ${state.service}`);
    await expect(inspector).toContainText('running');
    await expect(inspector).toContainText('Current recorded deployment service');
    expect(state.statusRequests).toEqual([`/api/v1/apps/runtime-owned/services/${state.service}/runtime`]);
    const runtime = page.getByRole('button', { name: 'Runtime', exact: true });
    await runtime.click();
    await runtime.focus();
    await page.keyboard.press('Enter');
    await expect(runtime).toHaveAttribute('aria-pressed', 'true');
    await expect(page.getByRole('log')).toContainText('current recorded service log');
    await expect(page.getByRole('log')).toContainText('not the historical run');
    expect(state.tickets).toEqual([`/ws/runtime/runtime-owned/${state.service}/logs`]);
    expect(state.sockets).toEqual(state.tickets);
    await page.screenshot({ path: testInfo.outputPath('recorded-runtime.png') });
    // Switch directly from live runtime to an approval selection, without reload.
    await page.getByRole('button', { name: 'Inspect recorded gate approval · approved', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Runtime', exact: true })).toHaveCount(0);
    await expect(page.getByRole('log')).not.toContainText('current recorded service log');
    await expect.poll(() => state.closed).toEqual(state.sockets);
    expect(state.statusRequests).toHaveLength(1);
    await page.getByRole('button', { name: 'Close inspector' }).click();
    await page.locator('.react-flow__node[data-id="deploy"]').click();
    await expect(page.getByRole('button', { name: 'Stage', exact: true })).toHaveAttribute('aria-pressed', 'true');
    expect(state.tickets).toHaveLength(1);
    expect(state.errors).toEqual([]);
  });
}

test('recorded runtime unsupported, build, approval and unrecorded selections make no runtime requests', async ({ page }) => {
  const state = await recordedRuntime(page);
  for (const id of ['build', 'approval', 'unrecorded', 'unsupported']) {
    await page.locator(`.react-flow__node[data-id="${id}"]`).click();
    await expect(page.getByRole('complementary')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Runtime', exact: true })).toHaveCount(0);
    await expect(page.getByRole('complementary')).not.toContainText('Current recorded deployment service');
    await expect(page.getByRole('log')).toContainText('recorded stage output');
    await page.getByRole('button', { name: 'Close inspector' }).click();
  }
  expect(state.statusRequests).toEqual([]);
  expect(state.tickets).toEqual([]);
  expect(state.sockets).toEqual([]);
  expect(state.errors).toEqual([]);
});

test('recorded runtime non-Docker status remains available without an unsupported live-log control', async ({ page }) => {
  const state = await recordedRuntime(page, { runtime: 'kubernetes' });
  await page.locator('.react-flow__node[data-id="deploy"]').click();
  await expect(page.getByRole('complementary')).toContainText('running');
  expect(state.statusRequests).toEqual(['/api/v1/apps/runtime-owned/services/deploy/runtime']);
  await expect(page.getByRole('button', { name: 'Runtime', exact: true })).toHaveCount(0);
  expect(state.tickets).toEqual([]);
  expect(state.sockets).toEqual([]);
});

test('recorded runtime controls require app context rather than a native pipeline definition alone', async ({ page }) => {
  const state = await recordedRuntime(page, { compose: true, appContext: false });
  await page.locator('.react-flow__node[data-id="deploy"]').click();
  await expect(page.getByRole('complementary')).not.toContainText('Current recorded deployment service');
  await expect(page.getByRole('button', { name: 'Runtime', exact: true })).toHaveCount(0);
  expect(state.statusRequests).toEqual([]);
  expect(state.tickets).toEqual([]);
  expect(state.sockets).toEqual([]);
});
