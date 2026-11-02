import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi } from './fixtures/api';
import type { CatalogApp, CatalogDeployment, CatalogPipeline, CatalogService } from '../src/types/catalog';
const capabilities = { edit: false, deploy: false, inspect: true, logs: false };
const production: CatalogDeployment = { id: 'prod-old-outside-history', provider: 'vercel', status: 'READY', environment: 'production', createdAt: '2026-10-01T00:00:00Z', url: 'https://cook-prod.vercel.app', productionUrls: ['https://cooker.example.com'], inspectorUrl: 'https://vercel.com/fixture/cooker/prod', sourceLabel: 'Uploaded Cooker landing bundle', artifactSha256: 'a'.repeat(64), aliasFreshness: { state: 'stale', observedAt: '2026-09-30T00:00:00Z', error: 'Production aliases could not be refreshed; any previous observation is retained' }, metadataFreshness: { state: 'stale', observedAt: '2026-10-01T00:00:00Z', error: 'Retained service metadata; provider detail lookup failed.' } };
const preview: CatalogDeployment = { id: 'preview-new', provider: 'vercel', status: 'BUILDING', environment: 'preview', createdAt: '2026-10-02T00:00:00Z', inspectorUrl: 'https://vercel.com/fixture/cooker/preview' };
const imported: CatalogApp = { id: 'vapp_fixture', name: 'Cooker landing', provider: 'vercel', management: 'vercel', href: '/apps/synced/vapp_fixture', pipelineId: 'vflow_fixture', framework: 'static', health: 'unknown', sourceLabel: production.sourceLabel, artifactSha256: production.artifactSha256, latestDeployment: preview, productionDeployment: production, freshness: { state: 'fresh', observedAt: '2026-10-02T00:00:00Z' }, metadataFreshness: { state: 'stale', observedAt: '2026-09-30T00:00:00Z', error: 'Retained project metadata; repository lookup failed.' }, capabilities };
const native: CatalogApp = { id: 'native', nativeAppId: 'native', name: 'n8n queue', provider: 'docker-host', management: 'cooker', href: '/apps/native', health: 'unknown', repository: 'fixture/n8n', branch: 'main', freshness: { state: 'recorded' }, capabilities: { ...capabilities, edit: true, deploy: true } };
const flow: CatalogPipeline = { id: 'vflow_fixture', name: 'Cooker landing flow', provider: 'vercel', management: 'vercel', kind: 'provider-flow', href: '/pipelines/synced/vflow_fixture', appId: imported.id, appName: imported.name, latestDeployment: preview, sourceLabel: production.sourceLabel, artifactSha256: production.artifactSha256, freshness: imported.freshness, capabilities };
const services: CatalogService[] = [
  { id: 'aggregate', appId: imported.id, appName: imported.name, appHref: imported.href, name: 'Application', provider: 'vercel', management: 'vercel', environment: 'production', status: 'ready', health: 'unknown', definition: 'aggregate', deploymentId: production.id, inspectorUrl: production.inspectorUrl, error: 'This is the application deployment. Vercel did not report its detailed component inventory.' },
  { id: 'site', appId: imported.id, appName: imported.name, appHref: imported.href, name: 'Landing site', provider: 'vercel', management: 'vercel', environment: 'production', status: 'ready', health: 'unknown', definition: 'deployed', deploymentId: production.id, inspectorUrl: production.inspectorUrl },
  { id: 'preview', appId: imported.id, appName: imported.name, appHref: imported.href, name: 'Preview site', provider: 'vercel', management: 'vercel', environment: 'preview', status: 'building', health: 'unknown', definition: 'deployed', deploymentId: preview.id, inspectorUrl: preview.inspectorUrl },
  { id: 'worker', appId: native.id, appName: native.name, appHref: native.href, name: 'n8n worker', provider: 'docker-host', management: 'cooker', environment: 'team-staging', status: 'configured', health: 'unknown', definition: 'configured' },
];
async function catalog(page: Page, partial = false) {
  await mockApi(page);
  const writes: string[] = [];
  page.on('request', (r) => { if (r.url().includes('/api/v1/') && !['GET', 'HEAD'].includes(r.method()) && !r.url().includes('ws-tickets')) writes.push(r.method() + ' ' + new URL(r.url()).pathname); });
  await page.route('**/api/v1/catalog/**', (route) => {
    const u = new URL(route.request().url()); const path = u.pathname; const q = u.searchParams;
    const sources = [{ provider: 'cooker', available: true }, { provider: 'vercel', available: !partial, error: partial ? 'Synchronization temporarily unavailable; retained records shown.' : undefined }];
    if (path.endsWith('/apps/vapp_fixture')) return route.fulfill({ json: { app: imported, pipeline: flow, services: services.filter((s) => s.appId === imported.id), deployments: [preview], sources } });
    if (path.endsWith('/pipelines/vflow_fixture')) return route.fulfill({ json: { pipeline: flow, app: imported, services, deployments: [preview], sources } });
    let items: (CatalogApp | CatalogPipeline | CatalogService)[] = path.endsWith('/apps') ? [native, imported] : path.endsWith('/services') ? services : [flow];
    items = items.filter((item) => (!q.get('management') || q.get('management') === item.management) && (!q.get('provider') || q.get('provider') === item.provider) && (!q.get('q') || item.name.toLowerCase().includes(q.get('q')!.toLowerCase())));
    if (path.endsWith('/services')) items = (items as CatalogService[]).filter((s) => (!q.get('appId') || q.get('appId') === s.appId) && (!q.get('status') || q.get('status') === s.status) && (!q.get('environment') || (q.get('environment') === 'production_first' ? s.management === 'cooker' || s.environment === 'production' : s.environment === q.get('environment'))));
    const limit = Number(q.get('limit')) || 24; const offset = Number(q.get('offset')) || 0;
    return route.fulfill({ json: { items: items.slice(offset, offset + limit), total: items.length, limit, offset, sources } });
  });
  return writes;
}

test('Apps combines native and imported ownership with isolated stale-source warning', async ({ page }) => {
  const writes = await catalog(page, true);
  await page.goto('/apps');
  await expect(page.getByRole('heading', { name: 'n8n queue', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Cooker landing', exact: true })).toBeVisible();
  await expect(page.getByText('Some sources are unavailable')).toBeVisible();
  await expect(page.getByText('Project metadata', { exact: true })).toBeVisible();
  await expect(page.getByText(/Retained project metadata; repository lookup failed/)).toContainText('stale · observed');
  await expect(page.getByRole('link', { name: 'Manage app →' })).toHaveAttribute('href', '/apps/native');
  await page.getByLabel('Managed by', { exact: true }).selectOption('vercel');
  await expect(page.getByRole('heading', { name: 'n8n queue', exact: true })).toHaveCount(0);
  await page.getByRole('link', { name: 'Inspect app →' }).click();
  await expect(page).toHaveURL(/\/apps\/synced\/vapp_fixture$/);
  await expect(page.getByRole('heading', { name: 'Production deployment', exact: true })).toBeVisible();
  await expect(page.getByText('Production alias observation', { exact: true })).toBeVisible();
  await expect(page.getByText('Last-observed production aliases', { exact: true })).toBeVisible();
  await expect(page.getByText(/Production aliases could not be refreshed/)).toContainText('stale · observed');
  await expect(page.getByText('Project metadata', { exact: true })).toBeVisible();
  await expect(page.getByText(/Retained project metadata; repository lookup failed/)).toContainText('stale · observed');
  await expect(page.getByRole('link', { name: 'cooker.example.com ↗' })).toHaveAttribute('href', 'https://cooker.example.com/');
  await expect(page.getByRole('button', { name: /Deploy|Delete|Edit/i })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Inspect / logs in Vercel ↗' }).first()).toHaveAttribute('href', production.inspectorUrl!);
  expect(writes).toEqual([]);
});

test('Services defaults to production plus native records and explicitly opts into previews', async ({ page }) => {
  await catalog(page);
  const request = page.waitForRequest((r) => r.url().includes('/catalog/services?'));
  await page.goto('/services');
  expect(new URL((await request).url()).searchParams.get('environment')).toBe('production_first');
  await expect(page.getByRole('heading', { name: 'Landing site', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Application', exact: true })).toBeVisible();
  await expect(page.getByText('Application deployment', { exact: true })).toBeVisible();
  await expect(page.getByText('This is the application deployment. Vercel did not report its detailed component inventory.', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'n8n worker', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Preview site', exact: true })).toHaveCount(0);
  await expect(page.getByText('docker-host · team-staging')).toBeVisible();
  await page.getByLabel('Environment', { exact: true }).selectOption('');
  await expect(page.getByRole('heading', { name: 'Latest previews' })).toBeVisible();
  await page.getByLabel('App', { exact: true }).selectOption('vapp_fixture');
  await expect(page.getByRole('heading', { name: 'n8n worker', exact: true })).toHaveCount(0);
  await expect(page).toHaveURL(/appId=vapp_fixture/);
  await page.reload();
  await expect(page.getByLabel('App', { exact: true })).toHaveValue('vapp_fixture');
  await expect(page.getByRole('heading', { name: 'n8n worker', exact: true })).toHaveCount(0);
  await page.getByLabel('App', { exact: true }).selectOption('native');
  await expect(page).toHaveURL(/appId=native/);
  await expect(page.getByRole('heading', { name: 'n8n worker', exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByLabel('App', { exact: true })).toHaveValue('vapp_fixture');
  await expect(page.getByRole('heading', { name: 'n8n worker', exact: true })).toHaveCount(0);
});

test('flow source belongs to selected deployment, with production outside history retained', async ({ page }) => {
  const writes = await catalog(page);
  await page.goto('/pipelines/synced/vflow_fixture');
  const graph = page.locator('.catalog-flow-panel');
  await expect(graph.getByText('Uploaded Cooker landing bundle', { exact: true })).toBeVisible();
  await expect(graph.getByText('a'.repeat(64), { exact: true })).toBeVisible();
  await expect(graph.getByText(/Retained service metadata/)).toContainText('stale · observed');
  await expect(graph.getByText(/Production aliases could not be refreshed/)).toContainText('stale · observed');
  await expect(graph.getByRole('link', { name: 'Deployment URL ↗' })).toHaveAttribute('href', new URL(production.url!).href);
  await expect(graph.getByRole('link', { name: 'cooker.example.com ↗' })).toHaveAttribute('href', 'https://cooker.example.com/');
  await graph.getByRole('button', { name: /2 · Deployment targets/ }).focus();
  await page.keyboard.press('Enter');
  await expect(graph.locator('.catalog-connection-explanation')).toContainText('Only confirmed production aliases');
  await page.getByLabel('Deployment to inspect').selectOption(preview.id);
  await expect(graph.getByText('Source not reported', { exact: true })).toBeVisible();
  await graph.getByRole('button', { name: /1 · Source relationship unknown/ }).click();
  await expect(graph.locator('.catalog-connection-explanation')).toContainText('does not identify this deployment’s source');
  await graph.getByRole('button', { name: 'Step list', exact: true }).click();
  await expect(graph.getByText('Source not reported', { exact: true })).toBeVisible();
  await expect(graph.locator('.catalog-connection-explanation')).toContainText('Source relationship unknown');
  await expect(graph.getByText('a'.repeat(64), { exact: true })).toHaveCount(0);
  await expect(graph.getByRole('link', { name: 'Deployment URL ↗' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^(Run|Save|Deploy|Approve)$/ })).toHaveCount(0);
  expect(writes).toEqual([]);
});

test('imported pipeline list navigates only to read-only flow', async ({ page }) => {
  const writes = await catalog(page);
  await page.goto('/pipelines');
  await page.getByLabel('Managed by', { exact: true }).selectOption('vercel');
  await page.getByRole('button', { name: 'Inspect provider flow →' }).click();
  await expect(page).toHaveURL(/\/pipelines\/synced\/vflow_fixture$/);
  expect(writes).toEqual([]);
});

test('catalog request failure remains retryable', async ({ page }) => {
  await catalog(page);
  let fail = true;
  await page.route('**/api/v1/catalog/apps?*', (route) => fail ? route.fulfill({ status: 503, json: { error: 'Catalog temporarily unavailable' } }) : route.fallback());
  await page.goto('/apps');
  await expect(page.getByRole('alert')).toContainText('Catalog temporarily unavailable');
  fail = false;
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Cooker landing', exact: true })).toBeVisible();
});

test('provider detail remains readable at 320px and has no serious accessibility violations', async ({ page }) => {
  await catalog(page);
  await page.setViewportSize({ width: 320, height: 820 });
  await page.goto('/pipelines/synced/vflow_fixture');
  await expect(page.locator('.catalog-flow-nodes>li')).toHaveCount(3);
  await expect(page.getByRole('heading', { name: 'Cooker landing flow', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations.filter((v) => ['critical', 'serious'].includes(v.impact ?? ''))).toEqual([]);
});

test('deployment deep links select exact history and missing ids never substitute production', async ({ page }) => {
  await catalog(page);
  await page.goto('/pipelines/synced/vflow_fixture?deployment=preview-new');
  const graph = page.locator('.catalog-flow-panel');
  await expect(graph.getByText('Source not reported', { exact: true })).toBeVisible();
  await expect(graph.getByText('Uploaded Cooker landing bundle', { exact: true })).toHaveCount(0);
  await page.goto('/pipelines/synced/vflow_fixture?deployment=not-retained');
  await expect(page.getByRole('status').filter({ hasText: 'another deployment’s source will not be substituted' })).toContainText('another deployment’s source will not be substituted');
  await expect(graph).toHaveCount(0);
  await page.getByLabel('Deployment to inspect').selectOption(production.id);
  await expect(graph.getByText('Uploaded Cooker landing bundle', { exact: true })).toBeVisible();
});

test('a slow old filter response cannot overwrite the current catalog query', async ({ page }) => {
  await catalog(page);
  let release: () => void = () => {};
  const blocked = new Promise<void>((resolve) => { release = resolve; });
  let started = false;
  await page.route('**/api/v1/catalog/apps?*', async (route) => {
    const q = new URL(route.request().url()).searchParams.get('q');
    if (q !== 'old') return route.fallback();
    started = true;
    await blocked;
    return route.fulfill({ json: { items: [{ ...native, name: 'Obsolete slow result' }], total: 1, limit: 24, offset: 0, sources: [] } });
  });
  await page.goto('/apps');
  await page.getByRole('searchbox', { name: 'Search', exact: true }).fill('old');
  await expect.poll(() => started).toBe(true);
  await page.getByRole('searchbox', { name: 'Search', exact: true }).fill('Cooker');
  await expect(page.getByRole('heading', { name: 'Cooker landing', exact: true })).toBeVisible();
  const oldResponse = page.waitForResponse((r) => new URL(r.url()).searchParams.get('q') === 'old');
  release();
  await oldResponse;
  await expect(page.getByRole('heading', { name: 'Obsolete slow result' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Cooker landing', exact: true })).toBeVisible();
});


test('provider choices survive empty filtered pages and selected dynamic filters remain visible', async ({ page }) => {
  await catalog(page);
  await page.goto('/services?environment=custom-retained&status=draining');
  await expect(page.getByRole('heading', { name: 'No service records in this view.' })).toBeVisible();
  await expect(page.getByLabel('Environment', { exact: true })).toHaveValue('custom-retained');
  await expect(page.getByLabel('Environment', { exact: true }).locator('option:checked')).toHaveText('custom-retained');
  await expect(page.getByLabel('Status', { exact: true }).locator('option:checked')).toHaveText('draining');
  const provider = page.getByLabel('Provider', { exact: true });
  await expect(provider.locator('option[value="docker-host"]')).toHaveCount(1);
  await expect(provider.locator('option[value="kubernetes"]')).toHaveCount(1);
  await page.getByRole('searchbox', { name: 'Search', exact: true }).fill('no results');
  await expect(page.getByRole('heading', { name: 'No service records in this view.' })).toBeVisible();
  await provider.selectOption('docker-host');
  await expect(provider).toHaveValue('docker-host');
  await expect(provider.locator('option[value="vercel"]')).toHaveCount(1);
  await page.goto('/pipelines');
  await page.getByRole('searchbox', { name: 'Search', exact: true }).fill('no results');
  await expect(page.getByRole('heading', { name: 'No pipelines match this view.' })).toBeVisible();
  await expect(provider.locator('option[value="cooker"]')).toHaveCount(1);
  await expect(provider.locator('option[value="vercel"]')).toHaveCount(1);
});


test('native resource ids in synced routes redirect to canonical Cooker pages', async ({ page }) => {
  const writes = await catalog(page);
  await page.route('**/api/v1/catalog/apps/native', (route) => route.fulfill({ json: { app: native, services: [], deployments: [], sources: [] } }));
  await page.route('**/api/v1/apps/native', (route) => route.fulfill({ json: { id: 'native', name: 'n8n queue', githubRepo: 'fixture/n8n', branch: 'main', healthStatus: 'unknown', deployTarget: { kind: 'docker-host' }, createdAt: '2026-10-01T00:00:00Z', updatedAt: '2026-10-01T00:00:00Z' } }));
  await page.goto('/apps/synced/native');
  await expect(page).toHaveURL(/\/apps\/native$/);
  await expect(page.getByRole('heading', { name: 'n8n queue', exact: true })).toBeVisible();
  await expect(page.getByText('Synced app · read-only', { exact: true })).toHaveCount(0);
  await page.route('**/api/v1/catalog/pipelines/gates', (route) => route.fulfill({ json: { pipeline: { id: 'gates', name: 'gates', management: 'cooker', provider: 'cooker', kind: 'cooker-dag', href: '/pipelines/gates/edit', freshness: { state: 'recorded' }, capabilities }, services: [], deployments: [], sources: [] } }));
  await page.goto('/pipelines/synced/gates');
  await expect(page).toHaveURL(/\/pipelines\/gates\/edit$/);
  await expect(page.getByText('Vercel provider flow', { exact: true })).toHaveCount(0);
  await expect(page.locator('.catalog-flow-panel')).toHaveCount(0);
  expect(writes).toEqual([]);
});
