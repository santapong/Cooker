import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { GATES_PIPELINE, GATES_RUN, mockApi } from './fixtures/api';
import type { Pipeline, PipelineRun } from '../src/types/pipeline';
import type { AppModel } from '../src/types/app';

// Integrated root UI / registry component fixtures only. No actual source,
// image, queue, account or deployment is read or mutated.
const pipeline: Pipeline = { ...GATES_PIPELINE, version: 8, source: { connectionId: 'owned-source', repository: 'owned/app', fullRef: 'refs/heads/main' }, stages: [{ id: 'a', name: 'Owned build', type: 'build', position: { x: 20, y: 20 }, config: { dockerfile: 'Dockerfile', registryConnectionIds: [] } }], edges: [] };
const registry = { id: 'owned-registry', name: 'Granted GHCR', url: 'ghcr.io', version: 4, hasPassword: true, allowedAppIds: ['app-a'], allowedPipelineIds: ['gates'] };
const denied = { ...registry, id: 'denied-registry', name: 'Denied connection', allowedAppIds: [], allowedPipelineIds: [] };
const source = { id: 'owned-source', name: 'Owned source', provider: 'github', enabled: true, version: 2, repositories: ['owned/app'] };
const run: PipelineRun = { ...GATES_RUN, status: 'success', pipelineVersion: 7, source: { source: { provider: 'github', connectionId: 'owned-source', host: 'github.com', repositoryId: 'provider-repo', repository: 'owned/app' }, fullRef: 'refs/heads/main', commitSha: 'a'.repeat(40), sourcePlanDigest: 'b'.repeat(64) } };
const app: AppModel = { id: 'app-a', name: 'Owned app', version: 12, githubRepo: 'owned/app', branch: 'main', hasWebhook: false, autoDeploy: false, deployTarget: { kind: 'docker-host', hostId: 'owned-host' }, buildPlan: { kind: 'dockerfile', path: 'Dockerfile', registryConnectionIds: [] }, canary: { strategy: 'rolling' }, createdAt: '2026-10-02T10:00:00Z', updatedAt: '2026-10-02T10:00:00Z' };

async function setup(page: Page, role: 'admin' | 'operator' | 'viewer' = 'operator', initial = pipeline) {
  await mockApi(page);
  await page.addInitScript(() => localStorage.setItem('cooker.local.token', 'owned-native-ui-session'));
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: role, email: `${role}@fixture.invalid`, name: role, role, roles: [role] } }));
  let current = structuredClone(initial); let currentRun = structuredClone(run); let sourceFailure = false; let currentApp = structuredClone(app); let appConflict = false;
  const saved: Pipeline[] = []; const savedApps: AppModel[] = []; const otherWrites: string[] = [];
  await page.route('**/api/v1/settings/registries', (route) => {
    if (route.request().method() !== 'GET') otherWrites.push(route.request().method());
    return route.fulfill({ json: [registry, denied] });
  });
  await page.route('**/api/v1/apps/source-connections', (route) => route.fulfill(sourceFailure ? { status: 503, json: { error: 'owned-connection-error-canary' } } : { json: [source] }));
  await page.route('**/api/v1/pipelines/gates', (route) => {
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON() as Pipeline; saved.push(structuredClone(body)); current = { ...body, version: (current.version ?? 1) + 1 };
    } else if (route.request().method() !== 'GET') otherWrites.push(route.request().method());
    return route.fulfill({ json: current });
  });
  await page.route('**/api/v1/pipelines/gates/runs/r1', (route) => route.fulfill({ json: currentRun }));
  await page.route('**/api/v1/apps/app-a', (route) => {
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON() as AppModel; savedApps.push(structuredClone(body));
      if (appConflict) { appConflict = false; currentApp = { ...currentApp, version: 13 }; return route.fulfill({ status: 409, json: { error: 'owned-app-error-canary' } }); }
      currentApp = { ...body, version: (currentApp.version ?? 1) + 1 };
    } else if (route.request().method() !== 'GET') otherWrites.push(route.request().method());
    return route.fulfill({ json: currentApp });
  });
  await page.route('**/api/v1/apps/app-a/deploys?*', (route) => route.fulfill({ json: { deploys: [] } }));
  await page.route('**/api/v1/apps/app-a/drift', (route) => route.fulfill({ json: { status: 'unknown' } }));
  await page.route('**/api/v1/catalog/apps/app-a', (route) => route.fulfill({ json: { app: { id: 'app-a', services: [] }, sources: [] } }));
  await page.route('**/api/v1/apps/app-a/triggers', (route) => route.fulfill({ json: { rules: [] } }));
  return { saved, savedApps, otherWrites, sourceFailure: (failed: boolean) => { sourceFailure = failed; }, checkout: () => { currentRun = { ...currentRun, source: { ...currentRun.source!, checkedOutAt: '2026-10-02T10:00:00Z' } }; }, running: () => { currentRun = { ...currentRun, status: 'running' }; }, appConflict: () => { appConflict = true; } };
}
async function inspect(page: Page, role: string) {
  await page.goto('/pipelines/gates/edit');
  await page.getByRole('button', { name: 'Step list', exact: true }).click();
  await page.locator('.pipeline-list-stage').filter({ hasText: 'Owned build' }).click();
  const inspector = page.getByRole('complementary', { name: 'Stage Owned build', exact: true });
  await inspector.getByText(role === 'viewer' ? 'Step configuration' : 'Edit step and connections', { exact: true }).click();
  return inspector;
}

test('native editor saves exact source ref and registry IDs without starting a run', async ({ page }) => {
  const fixture = await setup(page); const inspector = await inspect(page, 'operator');
  await expect(inspector.getByText(/Anonymous access — no saved registry credentials/)).toBeVisible();
  await page.getByRole('textbox', { name: /^Full branch or tag ref/ }).fill('refs/tags/v1.2.3');
  await inspector.getByRole('checkbox', { name: /^Granted GHCR/ }).check();
  await expect(inspector.getByRole('checkbox', { name: /^Denied connection/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  expect(fixture.saved).toHaveLength(1);
  expect(fixture.saved[0].version).toBe(8);
  expect(fixture.saved[0].source).toEqual({ connectionId: 'owned-source', repository: 'owned/app', fullRef: 'refs/tags/v1.2.3' });
  expect(fixture.saved[0].stages[0].config.registryConnectionIds).toEqual(['owned-registry']);
  expect(fixture.otherWrites).toEqual([]);
});

test('viewer can inspect source and granted registry metadata without editing or saving', async ({ page }) => {
  const fixture = await setup(page, 'viewer'); const inspector = await inspect(page, 'viewer');
  await expect(page.getByRole('textbox', { name: /^Full branch or tag ref/ })).toBeDisabled();
  await expect(inspector.getByRole('checkbox', { name: /^Granted GHCR/ })).toBeDisabled();
  await expect(inspector.getByLabel('Stage name', { exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Run pipeline', exact: true })).toHaveCount(0);
  expect(fixture.saved).toEqual([]); expect(fixture.otherWrites).toEqual([]);
});

test('unavailable source and unverified/revoked registry selections survive unrelated saves', async ({ page }) => {
  const initial = structuredClone(pipeline);
  initial.source = { connectionId: 'missing-source', repository: 'owned/missing', fullRef: 'refs/heads/main' };
  initial.stages[0].config.registryConnectionIds = ['missing-registry', 'denied-registry'];
  const fixture = await setup(page, 'operator', initial); const inspector = await inspect(page, 'operator');
  await expect(page.getByRole('combobox', { name: 'Source connection', exact: true })).toHaveValue('missing-source');
  await expect(page.getByText('The selected connection is unavailable. Ask an administrator to check it.', { exact: true })).toBeVisible();
  await expect(inspector).toContainText('missing-registry — access is unverified');
  await expect(inspector).toContainText('Denied connection — access is not granted');
  await inspector.getByLabel('Stage name', { exact: true }).fill('Renamed owned build');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  expect(fixture.saved[0].source).toEqual(initial.source);
  expect(fixture.saved[0].stages[0].config.registryConnectionIds).toEqual(['missing-registry', 'denied-registry']);
  expect(fixture.otherWrites).toEqual([]);
});

test('source read failure is safely explained and retry retains the existing binding', async ({ page }) => {
  const fixture = await setup(page); fixture.sourceFailure(true); await page.goto('/pipelines/gates/edit');
  const sourcePanel = page.locator('.pipeline-source-binding');
  await expect(sourcePanel.getByRole('alert')).toContainText('Source connections could not be loaded');
  await expect(sourcePanel).not.toContainText('owned-connection-error-canary');
  await expect(sourcePanel.getByRole('textbox', { name: /^Full branch or tag ref/ })).toBeDisabled();
  fixture.sourceFailure(false); await sourcePanel.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(sourcePanel.getByRole('textbox', { name: /^Full branch or tag ref/ })).toBeEnabled();
  await expect(sourcePanel.getByRole('textbox', { name: /^Full branch or tag ref/ })).toHaveValue('refs/heads/main');
  expect(fixture.saved).toEqual([]);
});

test('run receipt separates acceptance from verified checkout and labels the current graph version', async ({ page }) => {
  const fixture = await setup(page, 'viewer'); await page.goto('/pipelines/gates/runs/r1');
  const receipt = page.locator('.run-source-receipt'); await receipt.locator('summary').click();
  await expect(receipt).toContainText('Verified checkout has not been recorded');
  await expect(receipt).toContainText('Accepted pipeline version: 7 · Current version: 8');
  await expect(receipt).toContainText('does not reconstruct the accepted graph');
  await expect(receipt).toContainText('a'.repeat(40));
  await expect(page.getByRole('button', { name: '▶ Re-run', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Promote', exact: true })).toHaveCount(0);
  fixture.checkout(); fixture.running(); await page.reload(); await receipt.locator('summary').click();
  await expect(receipt).toContainText('The worker verified checkout of this accepted commit.');
  await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toHaveCount(0);
  expect(fixture.otherWrites).toEqual([]);
});

test('saved App selections retain a stale draft on 409, and only explicit reload changes its base version', async ({ page }) => {
  const fixture = await setup(page); fixture.appConflict(); await page.goto('/apps/app-a');
  const access = page.getByRole('group', { name: 'Registry access', exact: true });
  await access.getByRole('checkbox', { name: /^Granted GHCR/ }).check();
  await page.getByRole('button', { name: 'Save registry selections', exact: true }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'Registry selections could not be saved' })).toContainText('Your draft is retained');
  await expect(access.getByRole('checkbox', { name: /^Granted GHCR/ })).toBeChecked();
  await expect(page.locator('body')).not.toContainText('owned-app-error-canary');
  expect(fixture.savedApps).toHaveLength(1); expect(fixture.savedApps[0].version).toBe(12);
  await page.getByRole('button', { name: 'Reload selections', exact: true }).click();
  await expect(access.getByRole('checkbox', { name: /^Granted GHCR/ })).not.toBeChecked();
  await access.getByRole('checkbox', { name: /^Granted GHCR/ }).check();
  await page.getByRole('button', { name: 'Save registry selections', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save registry selections', exact: true })).toHaveCount(0);
  expect(fixture.savedApps[1].version).toBe(13);
  expect(fixture.savedApps[1].buildPlan?.registryConnectionIds).toEqual(['owned-registry']);
  expect(fixture.otherWrites).toEqual([]);
});

test('viewer App registry and deployment controls respect existing role policy', async ({ page }) => {
  const fixture = await setup(page, 'viewer'); await page.goto('/apps/app-a');
  await expect(page.getByRole('group', { name: 'Registry access', exact: true }).getByRole('checkbox', { name: /^Granted GHCR/ })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Save registry selections', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: '▶ Deploy', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Delete', exact: true })).toHaveCount(0);
  expect(fixture.savedApps).toEqual([]); expect(fixture.otherWrites).toEqual([]);
});

for (const theme of ['dark', 'light']) test(`${theme} native source and registry controls support narrow keyboard inspection`, async ({ page }, testInfo) => {
  const fixture = await setup(page);
  await page.addInitScript((theme) => localStorage.setItem('cooker-ui', JSON.stringify({ state: { themeMode: theme, calmMode: true }, version: 0 })), theme);
  await page.setViewportSize({ width: 390, height: 844 }); const inspector = await inspect(page, 'operator');
  await inspector.getByRole('checkbox', { name: /^Granted GHCR/ }).focus(); await page.keyboard.press('Space');
  await expect(inspector.getByRole('checkbox', { name: /^Granted GHCR/ })).toBeChecked();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).include('.registry-selection').analyze()).violations.filter((v) => ['critical', 'serious'].includes(v.impact ?? ''))).toEqual([]);
  await page.screenshot({ path: testInfo.outputPath(`native-authority-${theme}.png`), fullPage: true });
  expect(fixture.saved).toEqual([]); expect(fixture.otherWrites).toEqual([]);
});
