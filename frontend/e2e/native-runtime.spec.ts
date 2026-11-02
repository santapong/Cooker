import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi } from './fixtures/api';
import type { NativeRuntimeLogPage, NativeRuntimeSubject } from '../src/types/nativeRuntime';

// Presentation and role fixtures only. No worker, Docker or provider runs here.
const subject: NativeRuntimeSubject = { kind: 'native-stage', pipelineId: 'native-fixture', runId: 'recorded-run', stageId: 'deploy' };
const containerId = 'a'.repeat(64), imageId = `sha256:${'b'.repeat(64)}`;
const receipt = { version: 1, runtime: 'docker', runId: subject.runId, stageId: subject.stageId, containerId, imageId, imageReference: 'fixture/image:one', name: 'recorded-web', capturedAt: '2026-10-05T00:00:00Z' };
const stagePath = '/api/v1/pipelines/native-fixture/runs/recorded-run/stages/deploy';

function snapshot(offset = 0): NativeRuntimeLogPage {
  return { subject, sessionId: 'native-snapshot', runId: subject.runId, containerId, since: '2026-10-05T00:00:00Z', until: '2026-10-05T01:00:00Z', expiresAt: new Date(Date.now() + 600000).toISOString(), offset, nextOffset: offset === 0 ? 200 : undefined, total: 205, tailLimitReached: true, coverage: 'Bounded Docker tail in capture order.', entries: Array.from({ length: offset === 0 ? 200 : 5 }, (_, index) => ({ sequence: offset + index + 1, message: index === 0 ? 'known [REDACTED] <img src=x onerror=alert(1)>' : `native marker ${offset + index + 1}` })) };
}

async function setup(page: Page, options: { theme?: string; role?: string; removed?: boolean; unavailable?: boolean; mismatch?: boolean } = {}) {
  await mockApi(page);
  const theme = options.theme ?? 'dark', role = options.role ?? 'operator';
  await page.addInitScript(({ theme }) => {
    localStorage.setItem('cooker.local.token', 'native-runtime-fixture');
    localStorage.setItem('cooker-ui', JSON.stringify({ state: { themeMode: theme, mode: 'pro', calmMode: true }, version: 0 }));
  }, { theme });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: 'native-user', email: 'native@fixture.invalid', roles: [role], role } }));
  const deploy = { id: 'deploy', name: 'Current website label', type: 'deploy', config: { deployRuntime: 'docker', runtimeName: 'edited-name' }, position: { x: 120, y: 100 } };
  const build = { id: 'build', name: 'Build image', type: 'build', config: {}, position: { x: 430, y: 100 } };
  await page.route('**/api/v1/pipelines/native-fixture', (route) => route.fulfill({ json: { id: 'native-fixture', name: 'Native runtime fixture', stages: options.removed ? [build] : [deploy, build], edges: [], variables: {}, version: 2 } }));
  await page.route('**/api/v1/pipelines/native-fixture/runs/recorded-run', (route) => route.fulfill({ json: { id: 'recorded-run', pipelineId: 'native-fixture', status: 'success', stageRuns: [{ stageId: 'deploy', status: 'success', runtimeReceipt: receipt }, { stageId: 'build', status: 'success' }], variables: {}, environmentStatuses: [], pipelineVersion: 1 } }));
  await page.route('**/api/v1/pipelines/native-fixture/runs/recorded-run/stage-approvals', (route) => route.fulfill({ json: { runId: 'recorded-run', gates: [] } }));
  await page.route('**/api/v1/pipelines/native-fixture/runs/recorded-run/logs/*', (route) => route.fulfill({ json: { logs: 'recorded deployment step output' } }));
  const requests: string[] = [], tickets: string[] = [], sockets: string[] = [], closed: string[] = [], errors: string[] = [];
  let refused = false;
  await page.route(`**${stagePath}/runtime`, (route) => {
    requests.push('status');
    return route.fulfill({ json: { subject, recordedStageStatus: 'success', binding: options.unavailable ? 'unavailable' : 'verified', runtime: 'docker', containerId: options.mismatch ? 'c'.repeat(64) : containerId, name: receipt.name, imageId, imageReference: receipt.imageReference, state: 'running', health: 'not_configured', observedAt: '2026-10-05T01:00:00Z', supportedActions: ['inspect'] } });
  });
  await page.route(`**${stagePath}/log-replicas`, (route) => { requests.push('replicas'); return route.fulfill({ json: { subject, runId: subject.runId, replicas: [{ containerId, name: receipt.name, state: 'running' }] } }); });
  await page.route(`**${stagePath}/log-sessions**`, (route) => {
    const request = route.request(), url = new URL(request.url());
    requests.push(`${request.method()} ${url.pathname}${url.search}`);
    if (request.method() === 'DELETE') return route.fulfill({ status: 204 });
    if (request.method() === 'POST') return route.fulfill({ status: 201, json: snapshot() });
    if (refused) return route.fulfill({ status: 503, json: { error: 'private-refusal-canary' } });
    if (url.pathname.endsWith('/download')) return route.fulfill({ contentType: 'text/plain', body: 'known [REDACTED]\n' });
    return route.fulfill({ json: snapshot(Number(url.searchParams.get('offset') ?? 0)) });
  });
  await page.route('**/api/v1/ws-tickets', (route) => {
    const path = route.request().postDataJSON().path as string;
    if (!path.startsWith('/ws/pipelines/')) return route.fulfill({ status: 403, json: { error: 'fixture stage stream disabled' } });
    tickets.push(path);
    return route.fulfill({ json: { ticket: 'native-fixture-ticket' } });
  });
  await page.routeWebSocket('**/ws/pipelines/**', (ws) => {
    sockets.push(new URL(ws.url()).pathname);
    ws.onClose(() => closed.push(new URL(ws.url()).pathname));
    ws.send('exact recorded native container log');
  });
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/pipelines/native-fixture/runs/recorded-run');
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Native runtime fixture');
  await page.getByRole('button', { name: 'Inspect recorded deployment deploy', exact: true }).click();
  return { requests, tickets, sockets, closed, errors, refuse: () => { refused = true; } };
}

for (const theme of ['dark', 'light']) for (const width of [1440, 390]) {
  test(`native recorded status, log snapshot and live controls ${theme} ${width}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
    const state = await setup(page, { theme });
    const inspector = page.getByRole('complementary', { name: 'Stage Current website label' });
    await expect(inspector).toContainText('No health check configured');
    await expect(inspector).toContainText('A successful deployment step does not confirm its current health');
    await inspector.getByRole('button', { name: 'Browse recorded Docker logs' }).click();
    await inspector.getByRole('button', { name: 'Capture snapshot' }).click();
    await expect(inspector.getByLabel('deploy captured log output')).toContainText('known [REDACTED] <img');
    await expect(inspector.locator('.service-log-output img')).toHaveCount(0);
    await inspector.getByRole('button', { name: 'Next lines' }).click();
    await expect(inspector).toContainText('201–205 of 205');
    await inspector.getByRole('button', { name: 'Previous lines' }).click();
    await inspector.getByRole('button', { name: 'Close log browser' }).click();
    await expect.poll(() => state.requests.some((request) => request.startsWith('DELETE'))).toBe(true);
    await page.getByRole('button', { name: 'Runtime', exact: true }).click();
    await expect(page.getByRole('log')).toContainText('exact recorded native container log');
    expect(state.tickets).toEqual(['/ws/pipelines/native-fixture/runs/recorded-run/stages/deploy/runtime-logs']);
    await page.getByRole('button', { name: 'Stage', exact: true }).click();
    await expect.poll(() => state.closed.length).toBe(1);
    await expect(page.getByRole('log')).not.toContainText('exact recorded native container log');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    await page.screenshot({ path: testInfo.outputPath('native-runtime.png') });
    expect(state.errors).toEqual([]);
    if (width === 1440) expect((await new AxeBuilder({ page }).include('.inspector').analyze()).violations.filter((v) => ['serious', 'critical'].includes(v.impact ?? ''))).toEqual([]);
  });
}

test('viewers inspect status without fetching replicas, snapshots or native tickets', async ({ page }) => {
  const state = await setup(page, { role: 'viewer' });
  await expect(page.getByRole('complementary')).toContainText('No health check configured');
  await expect(page.getByRole('complementary')).toContainText('Runtime logs require an operator or administrator role');
  await expect(page.getByRole('button', { name: 'Runtime', exact: true })).toHaveCount(0);
  expect(state.requests).toEqual(['status']);
  expect(state.tickets).toEqual([]);
});

test('a removed stage remains inspectable without reconstructing its private definition', async ({ page }) => {
  const state = await setup(page, { removed: true });
  await expect(page.getByRole('complementary', { name: 'Stage Recorded deployment stage' })).toContainText('No health check configured');
  await expect(page.getByRole('region', { name: 'Recorded deployment stages' })).toContainText('removed stages keep their recorded IDs');
  expect(state.requests).toEqual(['status']);
});

for (const mismatch of [false, true]) test(`unavailable or substituted identity does not enable logs ${mismatch}`, async ({ page }) => {
  const state = await setup(page, { unavailable: !mismatch, mismatch });
  await expect(page.getByRole('complementary')).toContainText(mismatch ? 'could not be verified' : 'No replacement container has been selected');
  await expect(page.getByRole('button', { name: 'Runtime', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Browse recorded Docker logs' })).toHaveCount(0);
  expect(state.requests).toEqual(['status']);
});

test('a generation refusal clears displayed snapshot bytes and hides provider error contents', async ({ page }) => {
  const state = await setup(page);
  const inspector = page.getByRole('complementary');
  await inspector.getByRole('button', { name: 'Browse recorded Docker logs' }).click();
  await inspector.getByRole('button', { name: 'Capture snapshot' }).click();
  await expect(inspector).toContainText('native marker 2');
  state.refuse();
  await inspector.getByRole('button', { name: 'Next lines' }).click();
  await expect(inspector).toContainText('Logs are unavailable for this deployment');
  await expect(inspector.locator('.service-log-output')).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText('private-refusal-canary');
});
