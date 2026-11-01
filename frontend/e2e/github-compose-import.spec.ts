import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi, mockCompose, TYPED_PIPELINE, GATES_RUN } from './fixtures/api';
import type { AppModel, RepositoryInspection } from '../src/types/app';

const SHA = 'a'.repeat(40);
const candidates = [
  { path: 'deploy/compose.yaml', services: ['api', 'db'] },
  { path: 'deploy/compose.prod.yaml', services: ['api'] },
  { path: 'tools/compose.test.yml', services: ['test'] },
];

function inspection(app: Partial<AppModel>, blocked = false): RepositoryInspection {
  const bound = !!app.deployTarget?.externalServices?.length;
  const prefix = app.deployTarget?.prefix || 'shop';
  return {
    commit: SHA, candidates, profiles: ['production', 'worker'], requiredVariables: ['APP_PORT'],
    graph: { services: [
      { name: 'api', image: '', build: { context: 'api', dockerfile: '../docker/api.Dockerfile', target: 'runtime' }, ports: ['8080:8080'], environment: { DATABASE_URL: '[configured]' }, dependsOn: ['db'], networks: ['default'], volumes: [], command: '', status: 'unknown', runtimeSize: app.deployTarget?.kind === 'ecs' ? 'Fargate: 1 vCPU, 2048 MiB' : undefined },
      { name: 'db', image: 'postgres:16', external: bound, ports: [], environment: {}, dependsOn: [], networks: ['default'], volumes: ['data:/var/lib/postgresql/data'], command: '', status: 'unknown' },
    ], connections: [{ source: 'api', target: 'db', type: 'depends_on', label: 'depends_on' }], networks: ['default'], volumes: ['data'] },
    buildFiles: [{ service: 'api', context: 'api', dockerfile: 'docker/api.Dockerfile', content: 'FROM alpine:3 AS runtime\nCMD ["/app"]' }],
    diagnostics: blocked ? [{ service: 'api', field: 'build', message: 'Build SSH forwarding needs a native build pipeline' }] : [],
    workloads: bound ? { api: `${prefix}-api` } : { api: `${prefix}-api`, db: `${prefix}-db` },
    yaml: 'name: shop\nservices:\n  api:\n    environment:\n      DATABASE_URL: "[configured]"', deployable: !blocked,
  };
}

async function setup(page: Page, options: { blocked?: boolean; conflict?: boolean } = {}) {
  await mockApi(page);
  await mockCompose(page);
  const inspections: Partial<AppModel>[] = [];
  let saved: AppModel | undefined;
  let conflict = !!options.conflict;
  await page.route('**/api/v1/apps**', async (route) => {
    const req = route.request(); const url = new URL(req.url()); const path = url.pathname.replace('/api/v1', '');
    if (path === '/apps/github/connection') return route.fulfill({ json: { configured: true, installUrl: 'https://github.com/apps/cooker-test/installations/new', installations: [{ id: 7, account: { login: 'acme' } }], message: 'Approved workspace installation' } });
    if (path === '/apps/github/repositories') return route.fulfill({ json: { repositories: [{ full_name: 'acme/shop', private: true, default_branch: 'develop' }], hasMore: false } });
    if (path === '/apps/github/revisions') return route.fulfill({ json: { revisions: [{ name: 'develop', commit: { sha: SHA } }], hasMore: false } });
    if (path === '/apps/deployment-capabilities') return route.fulfill({ json: { targets: [{ kind: 'docker-host', available: true }, { kind: 'kubernetes', available: true }, { kind: 'ecs', available: true }, { kind: 'cloud-run', available: false, reason: 'Cloud Run project is not configured' }] } });
    if (path === '/apps/inspect') { const app = req.postDataJSON() as Partial<AppModel>; inspections.push(app); return route.fulfill({ json: inspection(app, options.blocked) }); }
    if (path === '/apps' && req.method() === 'POST' || path === '/apps/saved-app' && req.method() === 'PUT') {
      if (conflict) { conflict = false; return route.fulfill({ status: 409, json: { error: 'deployment prefix is already used on this target' } }); }
      saved = { ...req.postDataJSON(), id: 'saved-app', version: 1, hasWebhook: false, healthStatus: 'unknown', createdAt: '2026-09-06T00:00:00Z', updatedAt: '2026-09-06T00:00:00Z' };
      return route.fulfill({ status: req.method() === 'POST' ? 201 : 200, json: saved });
    }
    if (path === '/apps/saved-app') return route.fulfill({ json: saved });
    if (path.endsWith('/deploys')) return route.fulfill({ json: { deploys: [] } });
    if (path.endsWith('/drift')) return route.fulfill({ json: { status: 'unknown', checkedAt: '2026-09-06T00:00:00Z' } });
    if (path.endsWith('/canary')) return route.fulfill({ json: { canary: null } });
    if (path === '/apps') return route.fulfill({ json: saved ? [saved] : [] });
    return route.fulfill({ status: 404, json: { error: `Unhandled app fixture: ${path}` } });
  });
  await page.route('**/api/v1/environments**', (route) => route.fulfill({ json: [{ id: 'prod', name: 'Production', order: 1, secretKeys: ['CLOUD_DB', 'DB_SSLMODE'], plainVars: {} }] }));
  return { inspections, saved: () => saved };
}

async function sourceAndPreview(page: Page) {
  await page.goto('/apps/new');
  await page.getByLabel('GitHub account', { exact: false }).selectOption('7');
  await page.getByLabel('Connected repository', { exact: false }).selectOption('acme/shop');
  await expect(page.getByLabel('Branch or tag', { exact: false })).toHaveValue('develop');
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await page.getByLabel('deploy/compose.prod.yaml', { exact: false }).check();
  await page.getByLabel('Compose profiles', { exact: false }).fill('production, worker');
  await page.getByLabel('Compose interpolation inputs', { exact: false }).fill('APP_PORT=8080');
  await page.getByRole('button', { name: 'Render Compose', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1 })).toBeInViewport();
  await expect(page.getByLabel('Deployment graph')).toBeVisible();
  // Direct wizard navigation must load the shared node/edge styles itself.
  await expect(page.locator('.stack-preview-map .star').first()).toHaveCSS('width', '48px');
  await expect(page.locator('.stack-preview-map .constellation-hit').first()).toHaveCSS('fill', 'none');
  await page.getByText('Dockerfile · docker/api.Dockerfile', { exact: true }).click();
  await expect(page.getByText('FROM alpine:3 AS runtime', { exact: false })).toBeVisible();
}

async function chooseECSAndDatabase(page: Page) {
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await page.getByLabel('Deployment prefix', { exact: false }).fill('shop-production');
  await page.getByRole('button', { name: 'AWS ECS · Fargate', exact: false }).click();
  await expect(page.getByRole('button', { name: 'Google Cloud Run', exact: false })).toBeDisabled();
  await page.getByLabel('Runtime environment', { exact: false }).selectOption('prod');
  const db = page.locator('.external-binding').filter({ has: page.getByRole('heading', { name: 'db', exact: true }) });
  await db.getByLabel('Use an existing database', { exact: false }).check();
  await db.getByLabel('Existing resource', { exact: false }).fill('my-project:asia-southeast1:shop-db');
  await db.getByLabel('Connection key bindings', { exact: false }).fill('DATABASE_URL=CLOUD_DB\nDB_SSLMODE=DB_SSLMODE');
  await page.getByRole('button', { name: 'Review deployment', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'GCP Cloud SQL', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'shop-production-api', exact: true })).toBeVisible();
}

test('imports private GitHub Compose, previews Dockerfile, binds Cloud SQL to ECS and reopens the saved revision', async ({ page }) => {
  // Includes the full wizard plus a fresh-browser catalogue/review round trip.
  test.setTimeout(60000);
  const fixture = await setup(page);
  await sourceAndPreview(page);
  await chooseECSAndDatabase(page);
  await page.screenshot({ path: 'test-results/compose-import-desktop.png', fullPage: true });
  await page.getByRole('button', { name: 'Save app', exact: true }).click();
  await expect(page).toHaveURL(/\/apps\/saved-app$/);
  const app = fixture.saved()!;
  expect(app.buildPlan).toMatchObject({ commit: SHA, installationId: 7, files: ['deploy/compose.yaml', 'deploy/compose.prod.yaml'], profiles: ['production', 'worker'], variables: { APP_PORT: '8080' } });
  expect(app.deployTarget).toMatchObject({ kind: 'ecs', prefix: 'shop-production', externalServices: [{ service: 'db', consumers: ['api'], environment: { DATABASE_URL: 'CLOUD_DB', DB_SSLMODE: 'DB_SSLMODE' } }] });
  expect(app.autoDeploy).toBe(false);
  await page.goto('/docker/compose');
  const shared = page.getByRole('region', { name: 'GitHub stacks' });
  await expect(shared.getByRole('link', { name: 'Open saved app shop' })).toBeVisible();
  await page.evaluate(() => localStorage.clear());
  await page.reload();
  await shared.getByRole('link', { name: 'Open saved app shop' }).click();
  await page.getByRole('link', { name: 'Review source & targets' }).click();
  await expect(page.getByLabel('GitHub repository', { exact: false })).toHaveValue('acme/shop');
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  await expect(page.getByLabel('Compose profiles', { exact: false })).toHaveValue('production, worker');
  await expect(page.getByLabel('Compose merge order').locator('li')).toHaveCount(2);
  expect(fixture.inspections.at(-1)?.buildPlan?.commit).toBe(SHA);
});

test('changing GitHub account to public clears the source pin and file selection', async ({ page }) => {
  const fixture = await setup(page);
  await sourceAndPreview(page);
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  await page.getByLabel('GitHub account', { exact: false }).selectOption('');
  await page.getByLabel('GitHub repository', { exact: false }).fill('public/another');
  await page.getByRole('button', { name: 'Continue', exact: true }).click();
  expect(fixture.inspections.at(-1)?.buildPlan).toMatchObject({ installationId: 0, files: [] });
  expect(fixture.inspections.at(-1)?.buildPlan?.commit).toBeUndefined();
  await expect(page.getByLabel('Compose profiles', { exact: false })).toHaveValue('');
});

test('unsupported Compose settings block saving and prefix conflicts remain editable', async ({ page }) => {
  await setup(page, { blocked: true });
  await sourceAndPreview(page);
  await chooseECSAndDatabase(page);
  await expect(page.getByText('Build SSH forwarding needs a native build pipeline', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save app', exact: true })).toBeDisabled();
});

test('prefix collision can be corrected without losing the reviewed source', async ({ page }) => {
  const fixture = await setup(page, { conflict: true });
  await sourceAndPreview(page);
  await chooseECSAndDatabase(page);
  await page.getByRole('button', { name: 'Save app', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('deployment prefix is already used');
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  await page.getByLabel('Deployment prefix', { exact: false }).fill('shop-uat');
  await page.getByRole('button', { name: 'Review deployment', exact: true }).click();
  await page.getByRole('button', { name: 'Save app', exact: true }).click();
  await expect(page).toHaveURL(/\/apps\/saved-app$/);
  expect(fixture.saved()?.deployTarget.prefix).toBe('shop-uat');
  expect(fixture.saved()?.buildPlan?.commit).toBe(SHA);
});

test('deployment review works at phone width and passes serious accessibility checks', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await setup(page);
  await sourceAndPreview(page);
  await chooseECSAndDatabase(page);
  await expect(page.getByRole('button', { name: 'Save app', exact: true })).toBeEnabled();
  await page.screenshot({ path: 'test-results/compose-import-phone.png', fullPage: true });
  const overflow = await page.evaluate(() => [...document.querySelectorAll('main *')].filter((el) => { const b = el.getBoundingClientRect(); return b.right > window.innerWidth + 1 && getComputedStyle(el).position !== 'absolute' && !el.closest('.react-flow, .starfield'); }).map((el) => ({ tag: el.tagName, class: el.className, width: el.getBoundingClientRect().width })).slice(0, 12));
  expect(overflow).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  const violations = (await new AxeBuilder({ page }).analyze()).violations.filter((v) => v.impact === 'critical' || v.impact === 'serious');
  expect(violations).toEqual([]);
  await page.screenshot({ path: 'test-results/compose-import-phone.png', fullPage: true });
});

test('deployment view opens during checkout and loads the generated graph without a reload', async ({ page }) => {
  await mockApi(page);
  let graphReads = 0;
  await page.route('**/api/v1/apps/pending-app', (route) => route.fulfill({ json: { id: 'pending-app', name: 'Shop', githubRepo: 'acme/shop', branch: 'main', deployTarget: { kind: 'ecs', prefix: 'shop' }, autoDeploy: false } }));
  await page.route('**/api/v1/pipelines/gates', (route) => {
    graphReads++;
    return route.fulfill({ json: graphReads === 1 ? { ...TYPED_PIPELINE, stages: [], edges: [] } : TYPED_PIPELINE });
  });
  await page.route('**/api/v1/pipelines/gates/runs/r1', (route) => route.fulfill({ json: graphReads === 1 ? { ...GATES_RUN, stageRuns: [] } : GATES_RUN }));
  await page.goto('/apps/pending-app/deployments/gates/r1');
  await expect(page.getByText('Inspecting the source revision…', { exact: false })).toBeVisible();
  await expect(page.locator('.react-flow__node')).toHaveCount(TYPED_PIPELINE.stages.length, { timeout: 10000 });
  await expect(page.getByRole('link', { name: 'App settings', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Re-run', exact: false })).toHaveCount(0);
});
