import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi } from './fixtures/api';
import type { VercelConnection, VercelLandingArtifact, VercelLandingOperation } from '../src/types/vercel';

// Publication UI contract evidence only: all server and provider replies are
// local fixtures. This suite cannot publish a real Vercel project or artifact.
const artifact: VercelLandingArtifact = { name: 'Cooker landing', fileCount: 7, bytes: 123456, sha256: 'a'.repeat(64), previewUrl: '/landing-preview/' };
const connection: VercelConnection = { id: 'conn-1', name: 'Personal projects', identity: { id: 'account-1', name: 'Fixture account', scope: 'personal' }, enabled: true, hasToken: true, version: 4, projectIds: [], syncing: false, createdAt: '2026-10-01T08:00:00Z', updatedAt: '2026-10-01T08:00:00Z' };
const alternate = { ...connection, id: 'conn-2', name: 'Second account', version: 7 };

async function setup(page: Page, options: { viewer?: boolean; lostDeploy?: boolean; lostBeforeQueue?: boolean; expired?: boolean; unavailable?: boolean } = {}) {
  await mockApi(page);
  if (options.viewer) {
    await page.addInitScript(() => localStorage.setItem('cooker.local.token', 'fixture-local-session'));
    await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: 'viewer', email: 'viewer@cooker.test', name: 'Viewer', role: 'viewer', roles: ['viewer'] } }));
  }
  let currentArtifact = { ...artifact };
  let operations: VercelLandingOperation[] = [];
  let reads = 0;
  const writes: { path: string; body: unknown }[] = [];
  await page.route('**/api/v1/cloud/inventory', (route) => route.fulfill({ json: { enabled: false, providers: [], results: [], fetchedAt: '2026-10-01T08:00:00Z' } }));
  await page.route('**/api/v1/integrations/vercel', (route) => route.fulfill({ json: { connections: [connection, alternate], projects: [], deployments: [], syncIntervalSeconds: 120, historyLimit: 100 } }));
  await page.context().route('**/landing-preview/', (route) => route.fulfill({ contentType: 'text/html', body: '<!doctype html><title>Cooker landing fixture</title><h1>Cooker landing fixture</h1>' }));
  await page.route('**/api/v1/integrations/vercel/landing', (route) => {
    reads++;
    if (options.unavailable) return route.fulfill({ status: 503, json: { error: 'Landing publishing requires the durable queue.' } });
    return route.fulfill({ json: { artifact: currentArtifact, operations } });
  });
  await page.route('**/api/v1/integrations/vercel/landing/operations/*', (route) => {
    const id = new URL(route.request().url()).pathname.split('/').pop();
    const operation = operations.find((item) => item.id === id);
    return operation ? route.fulfill({ json: { operation } }) : route.fulfill({ status: 404, json: { error: 'Unknown operation' } });
  });
  await page.route('**/api/v1/admin/integrations/vercel/landing/**', (route) => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1/admin/integrations/vercel/landing', '');
    const body = route.request().postDataJSON();
    writes.push({ path, body });
    if (path === '/review') {
      const chosen = body.connectionId === alternate.id ? alternate : connection;
      const operation: VercelLandingOperation = { id: `op-${operations.length + 1}`, connectionId: chosen.id, connectionVersion: chosen.version, accountName: chosen.identity.name, projectName: `cooker-landing-unique-${operations.length + 1}`, artifactSha256: currentArtifact.sha256, target: 'production', status: 'reviewed', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + (options.expired ? -1000 : 900000)).toISOString() };
      operations = [operation, ...operations];
      return route.fulfill({ json: { operation } });
    }
    const match = path.match(/^\/operations\/([^/]+)\/(deploy|check)$/);
    if (!match) return route.fulfill({ status: 404, json: { error: 'Unexpected publication fixture request' } });
    const index = operations.findIndex((operation) => operation.id === match[1]);
    if (index < 0) return route.fulfill({ status: 404, json: { error: 'Unknown operation' } });
    if (match[2] === 'deploy') {
      if (options.lostBeforeQueue && writes.filter((write) => write.path.endsWith('/deploy')).length === 1) return route.abort('failed');
      operations[index] = { ...operations[index], status: options.lostDeploy ? 'needs_attention' : 'checking', projectId: 'prj-dedicated', ...(options.lostDeploy ? { message: 'Provider response was lost; outcome needs checking.' } : { deploymentId: 'dpl-landing' }) };
      if (options.lostDeploy) return route.abort('failed');
    } else if (operations[index].status === 'needs_attention') operations[index] = { ...operations[index], status: 'succeeded', deploymentId: 'dpl-landing', url: 'https://cooker-landing.example.test', message: 'Vercel confirmed the deployment is ready.' };
    return route.fulfill({ json: { operation: operations[index] } });
  });
  return { writes, reads: () => reads, operations: () => operations, changeArtifact: () => { currentArtifact = { ...artifact, sha256: 'b'.repeat(64) }; }, markPublished: () => { operations = operations.map((operation) => ({ ...operation, status: 'succeeded', url: 'https://cooker-landing.example.test' })); } };
}

function panel(page: Page) { return page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Publish Cooker landing', exact: true }) }); }
async function review(page: Page) {
  await page.goto('/cloud');
  await panel(page).getByRole('combobox', { name: 'Publish to Vercel account or team', exact: false }).selectOption(connection.id);
  await panel(page).getByRole('button', { name: 'Review deployment', exact: true }).click();
  await expect(panel(page).getByRole('heading', { name: 'Review before publishing' })).toBeVisible();
}

test('requires an explicit account choice and exact review before creating the first production release', async ({ page }) => {
  const fixture = await setup(page);
  await page.goto('/cloud');
  const publish = panel(page);
  await expect(publish).toContainText('This publishes only the landing page and its visual assets. The Cooker application and backend are not included.');
  await expect(publish.getByRole('button', { name: 'Review deployment', exact: true })).toBeDisabled();
  expect(fixture.writes).toEqual([]);
  const popup = page.waitForEvent('popup');
  await publish.getByRole('link', { name: 'Preview landing' }).click();
  const preview = await popup;
  await expect(preview.getByRole('heading', { name: 'Cooker landing fixture' })).toBeVisible();
  expect(preview.url()).not.toContain('token');
  await preview.close();
  await publish.getByRole('combobox', { name: 'Publish to Vercel account or team', exact: false }).selectOption(connection.id);
  await publish.getByRole('button', { name: 'Review deployment', exact: true }).click();
  const reviewed = publish.getByLabel('Landing deployment review');
  await expect(reviewed).toContainText('Production · first release');
  await expect(reviewed).toContainText(artifact.sha256);
  await expect(reviewed).toContainText('7 files · 123,456 bytes');
  await expect(reviewed).toContainText('cooker-landing-unique-1');
  expect(fixture.writes).toEqual([{ path: '/review', body: { connectionId: connection.id, version: connection.version } }]);
  await publish.getByRole('button', { name: 'Create project & publish landing', exact: true }).click();
  await expect(publish.getByText('Checking Vercel', { exact: true })).toBeVisible();
  expect(fixture.writes[1]).toEqual({ path: '/operations/op-1/deploy', body: {} });
  await expect(publish.getByText('Cooker operation', { exact: false })).toContainText('op-1');
  await expect(publish.getByText('Vercel deployment', { exact: false })).toContainText('dpl-landing');
  await expect(publish.getByRole('button', { name: 'Check provider', exact: true })).toHaveCount(0);
  fixture.markPublished();
  await publish.getByRole('button', { name: 'Refresh status', exact: true }).click();
  await expect(publish.getByText('Published', { exact: true })).toBeVisible();
  await expect(publish.getByRole('link', { name: 'Open published landing' })).toHaveAttribute('href', 'https://cooker-landing.example.test/');
  expect(fixture.writes.map((write) => write.path)).toEqual(['/review', '/operations/op-1/deploy']);
});

test('changing the account invalidates the displayed review without publishing it', async ({ page }) => {
  const fixture = await setup(page);
  await review(page);
  await panel(page).getByRole('combobox', { name: 'Publish to Vercel account or team', exact: false }).selectOption(alternate.id);
  await expect(panel(page).getByRole('button', { name: 'Create project & publish landing', exact: true })).toHaveCount(0);
  expect(fixture.writes).toHaveLength(1);
});

test('lost deploy responses recover the existing receipt after reload and offer check without a duplicate submission', async ({ page }) => {
  const fixture = await setup(page, { lostDeploy: true });
  await review(page);
  await panel(page).getByRole('button', { name: 'Create project & publish landing', exact: true }).click();
  await expect(panel(page).getByText('Submission is not confirmed.', { exact: false })).toBeVisible();
  await expect(panel(page).getByRole('button', { name: 'Create project & publish landing', exact: true })).toHaveCount(0);
  await page.reload();
  await expect(panel(page).getByText('Needs checking', { exact: true })).toBeVisible();
  await panel(page).getByRole('combobox', { name: 'Publish to Vercel account or team', exact: false }).selectOption(connection.id);
  await expect(panel(page).getByRole('button', { name: 'Review deployment', exact: true })).toBeDisabled();
  await expect(panel(page).getByRole('button', { name: /Retry/ })).toHaveCount(0);
  await panel(page).getByRole('button', { name: 'Check provider', exact: true }).click();
  await expect(panel(page).getByText('Published', { exact: true })).toBeVisible();
  expect(fixture.operations()).toHaveLength(1);
  expect(fixture.writes.map((write) => write.path)).toEqual(['/review', '/operations/op-1/deploy', '/operations/op-1/check']);
});

test('a lost request still shown as reviewed can only be explicitly submitted under the same operation ID', async ({ page }) => {
  const fixture = await setup(page, { lostBeforeQueue: true });
  await review(page);
  await panel(page).getByRole('button', { name: 'Create project & publish landing', exact: true }).click();
  await expect(panel(page).getByText('Submission is not confirmed.', { exact: false })).toBeVisible();
  await page.reload();
  await panel(page).getByRole('button', { name: 'Open review', exact: true }).click();
  await expect(panel(page).getByText('Cooker still shows this request as reviewed.', { exact: false })).toBeVisible();
  await expect(panel(page).getByRole('button', { name: 'Check provider', exact: true })).toHaveCount(0);
  expect(fixture.writes).toHaveLength(2);
  await panel(page).getByRole('button', { name: 'Submit reviewed request', exact: true }).click();
  await expect(panel(page).getByText('Checking Vercel', { exact: true })).toBeVisible();
  expect(fixture.operations()).toHaveLength(1);
  expect(fixture.writes.map((write) => write.path)).toEqual(['/review', '/operations/op-1/deploy', '/operations/op-1/deploy']);
});

test('expired reviews and changed artifact hashes cannot publish', async ({ page }) => {
  const fixture = await setup(page);
  await page.clock.install();
  await review(page);
  fixture.changeArtifact();
  const reads = fixture.reads();
  await page.clock.fastForward(5_000);
  await expect.poll(fixture.reads).toBeGreaterThan(reads);
  await expect(panel(page).getByRole('button', { name: 'Create project & publish landing', exact: true })).toBeDisabled();
  expect(fixture.writes).toHaveLength(1);
});

test('an expired server review is visibly blocked', async ({ page }) => {
  const fixture = await setup(page, { expired: true });
  await review(page);
  await expect(panel(page).getByRole('button', { name: 'Create project & publish landing', exact: true })).toBeDisabled();
  await expect(panel(page).getByText('This review expired', { exact: false })).toBeVisible();
  expect(fixture.writes).toHaveLength(1);
});

test('viewers see the artifact but no publication mutations', async ({ page }) => {
  const fixture = await setup(page, { viewer: true });
  await page.goto('/cloud');
  await expect(panel(page).getByRole('link', { name: 'Preview landing' })).toBeVisible();
  await expect(panel(page).getByRole('button', { name: 'Review deployment', exact: true })).toHaveCount(0);
  expect(fixture.writes).toEqual([]);
});

test('unavailable publication service stays separate from existing history', async ({ page }) => {
  const fixture = await setup(page, { unavailable: true });
  await page.goto('/cloud');
  await expect(panel(page).getByRole('alert')).toContainText('Landing publishing requires the durable queue.');
  await expect(page.getByRole('heading', { name: 'Vercel deployments', exact: true })).toBeVisible();
  await expect(panel(page).getByRole('button', { name: 'Review deployment', exact: true })).toHaveCount(0);
  expect(fixture.writes).toEqual([]);
});

test('publication review is accessible and fits a narrow screen', async ({ page }, testInfo) => {
  await setup(page);
  await review(page);
  await panel(page).screenshot({ path: testInfo.outputPath('landing-publication-review.png') });
  expect((await new AxeBuilder({ page }).include('main').analyze()).violations).toEqual([]);
  await page.setViewportSize({ width: 390, height: 844 });
  const publish = panel(page).getByRole('button', { name: 'Create project & publish landing', exact: true });
  await publish.scrollIntoViewIfNeeded();
  await expect(publish).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath('landing-publication-mobile.png') });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
