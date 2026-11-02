import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi } from './fixtures/api';
import type { RegistryConfig, RegistryGrantUpdate } from '../src/types/infra';

// Owned metadata/role fixtures only. Every API request is intercepted; no
// account, database, registry, credential store or deployment is contacted.
const initial: RegistryConfig = { id: 'owned-registry', name: 'Owned GHCR', url: 'ghcr.io', version: 5, hasPassword: true, allowedAppIds: ['app-a', 'outside-app'], allowedPipelineIds: ['pipeline-a'] };
const apps = [{ id: 'app-a', name: 'Owned app' }, ...Array.from({ length: 109 }, (_, n) => ({ id: `app-${n}`, name: `App ${n}` }))];
const pipelines = [{ id: 'pipeline-a', name: 'Owned native pipeline' }, { id: 'vflow_imported', name: 'Imported Vercel flow' }];

async function setup(page: Page, role = 'admin') {
  await mockApi(page);
  await page.addInitScript(() => localStorage.setItem('cooker.local.token', 'owned-registry-ui-session'));
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: role, name: role, email: `${role}@fixture.invalid`, role, roles: [role] } }));
  await page.route('**/api/v1/registry/repositories', (route) => route.fulfill({ json: { repositories: [], ociSpec: 'Owned fixture' } }));
  const writes: { method: string; body: RegistryGrantUpdate }[] = [];
  const targetReads: URL[] = [];
  let registry = structuredClone(initial); let conflict = false; let failedRead = 0; let failTargets = false;
  await page.route('**/api/v1/settings/registries**', (route) => {
    const request = route.request();
    if (request.method() === 'GET') return route.fulfill(failedRead ? { status: failedRead, json: { error: 'owned-error-canary-must-not-render' } } : { json: [registry, { id: 'legacy-registry', name: 'Legacy connection', url: 'legacy.invalid', hasPassword: true }] });
    const body = request.postDataJSON() as RegistryGrantUpdate;
    writes.push({ method: request.method(), body });
    if (request.method() !== 'PATCH' || role !== 'admin') return route.fulfill({ status: 403, json: { error: 'forbidden' } });
    if (conflict) {
      conflict = false; registry = { ...registry, version: 6, allowedAppIds: ['app-104'], allowedPipelineIds: [] };
      return route.fulfill({ status: 409, json: { error: 'owned-error-canary-must-not-render' } });
    }
    if (body.version !== registry.version) return route.fulfill({ status: 409, json: { error: 'version conflict' } });
    registry = { ...registry, ...body, version: body.version + 1 };
    return route.fulfill({ json: registry });
  });
  for (const [kind, items] of [['apps', apps], ['pipelines', pipelines]] as const) await page.route(`**/api/v1/${kind}?*`, (route) => {
    const url = new URL(route.request().url()); targetReads.push(url);
    if (failTargets && kind === 'apps') return route.fulfill({ status: 503, json: { error: 'owned-error-canary-must-not-render' } });
    const offset = Number(url.searchParams.get('offset')); const limit = Number(url.searchParams.get('limit'));
    return route.fulfill({ json: items.slice(offset, offset + limit) });
  });
  return { writes, targetReads, conflict: () => { conflict = true; }, failedRead: (status: number) => { failedRead = status; }, failedTargets: () => { failTargets = true; } };
}

const editor = (page: Page) => page.getByRole('form', { name: 'Registry access for Owned GHCR' });
async function openEditor(page: Page) {
  await page.goto('/registry');
  await page.getByRole('button', { name: 'Manage Owned GHCR access', exact: true }).click();
  await expect(editor(page).getByRole('checkbox', { name: 'App 104', exact: true })).toBeVisible();
}

test('admin grant edits read paged native targets and preserve unlisted grants without credential writes', async ({ page }) => {
  const fixture = await setup(page); await openEditor(page);
  const form = editor(page);
  await expect(form.getByRole('checkbox', { name: 'Existing app grant: outside-app (not in loaded inventory)', exact: true })).toBeChecked();
  await expect(form.getByText('Imported Vercel flow', { exact: true })).toHaveCount(0);
  await form.getByRole('checkbox', { name: 'App 104', exact: true }).check();
  await form.getByRole('button', { name: 'Save registry access', exact: true }).click();
  await expect(form).toHaveCount(0);
  expect(fixture.writes).toEqual([{ method: 'PATCH', body: { version: 5, allowedAppIds: ['app-a', 'outside-app', 'app-104'], allowedPipelineIds: ['pipeline-a'] } }]);
  expect(fixture.targetReads.filter((url) => url.pathname.endsWith('/apps')).map((url) => url.searchParams.get('offset'))).toEqual(['0', '100']);
  await expect(page.getByRole('button', { name: 'Manage Legacy connection access', exact: true })).toBeDisabled();
});

test('409 reloads current version and grants, preserves the conflict explanation, and never retries automatically', async ({ page }) => {
  const fixture = await setup(page); await openEditor(page); fixture.conflict();
  const form = editor(page);
  await form.getByRole('checkbox', { name: 'App 103', exact: true }).check();
  await form.getByRole('button', { name: 'Save registry access', exact: true }).click();
  await expect(form.getByRole('alert')).toContainText('latest grants have been reloaded');
  await expect(form.getByRole('checkbox', { name: 'Owned app', exact: true })).not.toBeChecked();
  await expect(form.getByRole('checkbox', { name: 'App 104', exact: true })).toBeChecked();
  await expect(form.getByRole('checkbox', { name: 'App 103', exact: true })).not.toBeChecked();
  await expect(form).toContainText('version 6'); expect(fixture.writes).toHaveLength(1);
  await expect(page.locator('body')).not.toContainText('owned-error-canary');
  await form.getByRole('button', { name: 'Save registry access', exact: true }).click();
  await expect(form).toHaveCount(0);
  expect(fixture.writes[1]).toEqual({ method: 'PATCH', body: { version: 6, allowedAppIds: ['app-104'], allowedPipelineIds: [] } });
});

test('revocation is explicit and sends empty grants for both ownership kinds', async ({ page }) => {
  const fixture = await setup(page); await openEditor(page);
  await editor(page).getByRole('button', { name: 'Revoke all access', exact: true }).click();
  expect(fixture.writes).toEqual([]);
  await editor(page).getByRole('button', { name: 'Confirm revoke all access?', exact: true }).click();
  await expect(editor(page)).toHaveCount(0);
  expect(fixture.writes).toEqual([{ method: 'PATCH', body: { version: 5, allowedAppIds: [], allowedPipelineIds: [] } }]);
});

test('failed registry reads retain labelled data and suppress mutation and unsafe error text', async ({ page }) => {
  const fixture = await setup(page); await page.goto('/registry');
  await expect(page.getByRole('button', { name: 'Manage Owned GHCR access', exact: true })).toBeEnabled();
  fixture.failedRead(503); await page.getByRole('button', { name: 'Refresh registry connections', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Registry connections' }).getByRole('alert')).toContainText('Last loaded information may be out of date');
  await expect(page.getByRole('button', { name: 'Manage Owned GHCR access', exact: true })).toBeDisabled();
  await expect(page.locator('body')).not.toContainText('owned-error-canary');
  fixture.failedRead(0); await page.getByRole('button', { name: 'Refresh registry connections', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Manage Owned GHCR access', exact: true })).toBeEnabled();
  expect(fixture.writes).toEqual([]);
});

test('unavailable target inventory never erases the current grant set', async ({ page }) => {
  const fixture = await setup(page); fixture.failedTargets(); await page.goto('/registry');
  await page.getByRole('button', { name: 'Manage Owned GHCR access', exact: true }).click();
  const form = editor(page);
  await expect(form.getByText('Existing app grant: outside-app (not in loaded inventory)', { exact: true })).toBeVisible();
  await expect(form.getByRole('alert')).toContainText('Existing grants remain selected');
  await expect(form).not.toContainText('owned-error-canary');
  await form.getByRole('button', { name: 'Save registry access', exact: true }).click();
  await expect(form).toHaveCount(0);
  expect(fixture.writes[0].body).toEqual({ version: 5, allowedAppIds: ['app-a', 'outside-app'], allowedPipelineIds: ['pipeline-a'] });
});

for (const role of ['viewer', 'operator']) test(`${role} can inspect registry metadata without administrative controls or target reads`, async ({ page }) => {
  const fixture = await setup(page, role); await page.goto('/registry');
  await expect(page.getByText('Only administrators can add, remove, or grant access to registry connections.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /Manage .* access|Add registry|Remove Owned GHCR/ })).toHaveCount(0);
  expect(fixture.targetReads).toEqual([]); expect(fixture.writes).toEqual([]);
});

for (const theme of ['dark', 'light']) test(`${theme} registry access form supports narrow layout, keyboard and accessibility`, async ({ page }, testInfo) => {
  const fixture = await setup(page);
  await page.addInitScript((theme) => localStorage.setItem('cooker-ui', JSON.stringify({ state: { themeMode: theme, calmMode: true }, version: 0 })), theme);
  await page.setViewportSize({ width: 390, height: 844 }); await openEditor(page);
  const form = editor(page);
  await form.getByRole('checkbox', { name: 'Owned app', exact: true }).focus();
  await page.keyboard.press('Space'); await expect(form.getByRole('checkbox', { name: 'Owned app', exact: true })).not.toBeChecked();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).include('.registry-grants').analyze()).violations.filter((v) => ['serious', 'critical'].includes(v.impact ?? ''))).toEqual([]);
  await page.screenshot({ path: testInfo.outputPath(`registry-grants-${theme}.png`), fullPage: true });
  expect(fixture.writes).toEqual([]);
});
