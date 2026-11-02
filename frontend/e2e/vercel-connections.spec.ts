import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi } from './fixtures/api';
import type { VercelConnection, VercelConnectionUpdate, VercelDeployment, VercelProject, VercelSnapshot } from '../src/types/vercel';

// Browser/API-contract fixtures only. No hosted Vercel account, token or
// deployment is contacted; backend provider acceptance is separate evidence.
const TOKEN = 'fixture-vercel-token-not-a-credential';
const base: VercelConnection = {
  id: 'vc-1', name: 'Product team', teamId: 'team_fixture', identity: { id: 'team_fixture', name: 'Product', scope: 'team' },
  enabled: true, hasToken: true, version: 3, projectIds: ['prj-shop'], syncing: false,
  createdAt: '2026-09-30T08:00:00Z', updatedAt: '2026-09-30T08:00:00Z', lastSuccessAt: '2026-09-30T08:00:00Z',
};
const history: VercelDeployment[] = [
  { connectionId: base.id, id: 'dpl-ready', projectId: 'prj-shop', name: 'Shop', url: 'https://shop.example.test', state: 'READY', target: 'production', gitBranch: 'main', gitCommitSHA: 'a'.repeat(40), createdAt: '2026-09-30T08:00:00Z' },
  { connectionId: base.id, id: 'dpl-failed', projectId: 'prj-api', name: 'API', url: 'javascript:alert(1)', state: 'ERROR', target: 'preview', gitBranch: 'feature/api', createdAt: '2026-09-30T07:00:00Z' },
];

async function setup(page: Page, options: { empty?: boolean; viewer?: boolean; discoveryProjects?: VercelProject[]; discoveryUnavailable?: boolean; coverage?: VercelConnection['coverage'] } = {}) {
  await mockApi(page);
  if (options.viewer) {
    await page.addInitScript(() => localStorage.setItem('cooker.local.token', 'fixture-local-auth'));
    await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: 'viewer', email: 'viewer@cooker.test', name: 'Viewer', role: 'viewer', roles: ['viewer'] } }));
  }
  await page.route('**/api/v1/admin/source-connections', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/v1/cloud/inventory', (route) => route.fulfill({ json: { enabled: false, providers: [], results: [], fetchedAt: '2026-09-30T08:00:00Z' } }));
  let connections = options.empty ? [] : [structuredClone({ ...base, coverage: options.coverage })];
  const writes: { method: string; body?: Record<string, unknown>; version?: string | null }[] = [];
  let snapshotReads = 0;
  let failSnapshots = false;
  let failSync = false;
  let conflict = false;
  const unexpected: string[] = [];
  await page.route('**/api/v1/integrations/vercel', (route) => {
    snapshotReads++;
    if (failSnapshots) return route.fulfill({ status: 503, json: { error: 'Vercel snapshot temporarily unavailable' } });
    const data: VercelSnapshot = { connections, projects: [{ connectionId: base.id, id: 'prj-shop', name: 'Shop', framework: 'nextjs' }, { connectionId: base.id, id: 'prj-api', name: 'API' }], deployments: connections.length ? history : [], syncIntervalSeconds: 120, historyLimit: 100 };
    return route.fulfill({ json: data });
  });
  await page.route('**/api/v1/admin/integrations/vercel/**', (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.replace('/api/v1/admin/integrations/vercel', '');
    if (path === '/connections' && req.method() === 'GET') return route.fulfill({ json: { connections } });
    if (path === '/connections' && req.method() === 'POST') {
      const input = req.postDataJSON() as Record<string, unknown>;
      writes.push({ method: req.method(), body: input });
      const connection = { ...base, name: String(input.name), version: 1, projectIds: [], lastSuccessAt: undefined,
        teamId: input.teamId as string | undefined, identity: input.teamId ? base.identity : { id: 'user_fixture', name: 'Fixture user', scope: 'personal' as const } };
      connections.push(connection);
      return route.fulfill({ json: { connection } });
    }
    if (path === `/connections/${base.id}/projects`) {
      if (options.discoveryUnavailable) return route.fulfill({ status: 503, json: { error: 'Discovery could not verify complete project coverage' } });
      return route.fulfill({ json: { projects: options.discoveryProjects ?? [{ id: 'prj-shop', name: 'Shop', framework: 'nextjs' }, { id: 'prj-api', name: 'API' }] } });
    }
    if (path === `/connections/${base.id}` && req.method() === 'PUT') {
      const input = req.postDataJSON() as VercelConnectionUpdate;
      writes.push({ method: req.method(), body: { ...input } });
      if (conflict) return route.fulfill({ status: 409, json: { error: 'Connection changed. Reload before saving.' } });
      const connection = { ...connections[0], ...input, version: input.version + 1 };
      delete connection.token;
      connections = [connection];
      return route.fulfill({ json: { connection } });
    }
    if (path === `/connections/${base.id}/sync`) {
      writes.push({ method: 'SYNC' });
      connections = connections.map((connection) => ({ ...connection, lastSuccessAt: failSync ? connection.lastSuccessAt : new Date().toISOString(), syncError: failSync ? 'Vercel token is no longer authorized' : undefined }));
      return route.fulfill({ json: { connection: connections[0] } });
    }
    if (path === `/connections/${base.id}` && req.method() === 'DELETE') {
      writes.push({ method: req.method(), version: url.searchParams.get('version') }); connections = [];
      return route.fulfill({ json: { disconnected: true } });
    }
    unexpected.push(`${req.method()} ${path}`);
    return route.fulfill({ status: 404, json: { error: 'Unexpected Vercel fixture request' } });
  });
  return { writes, unexpected, snapshotReads: () => snapshotReads, failSnapshots: () => { failSnapshots = true; }, failSync: () => { failSync = true; }, conflict: () => { conflict = true; } };
}

for (const team of [false, true]) {
  test(`connects ${team ? 'a team' : 'a personal account'} and explicitly selects projects without retaining the token`, async ({ page }) => {
    const fixture = await setup(page, { empty: true });
    await page.goto('/settings');
    await page.getByRole('button', { name: 'Connect Vercel', exact: true }).click();
    await page.getByLabel('Vercel connection name', { exact: true }).fill('My account');
    if (team) await page.getByLabel('Vercel team ID', { exact: false }).fill('team_fixture');
    await page.getByLabel('Vercel access token', { exact: false }).fill(TOKEN);
    await page.getByRole('button', { name: 'Verify & connect Vercel', exact: true }).click();
    await expect(page.getByRole('checkbox', { name: 'Shop', exact: false })).not.toBeChecked();
    expect(fixture.writes[0]).toEqual({ method: 'POST', body: { name: 'My account', token: TOKEN, ...(team ? { teamId: 'team_fixture' } : {}) } });
    await expect(page.getByLabel('Replacement Vercel token', { exact: false })).toHaveValue('');
    await page.getByRole('checkbox', { name: 'Shop', exact: false }).check();
    await page.getByRole('button', { name: 'Save Vercel settings', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Sync now', exact: true })).toBeEnabled();
    expect(fixture.writes[1]).toEqual({ method: 'PUT', body: { version: 1, name: 'My account', enabled: true, projectIds: ['prj-shop'] } });
    await expect(page.locator('body')).not.toContainText(TOKEN);
    expect(await page.evaluate(() => JSON.stringify([Object.entries(localStorage), Object.entries(sessionStorage)]))).not.toContain(TOKEN);
    expect(page.url()).not.toContain(TOKEN);
    await page.getByRole('button', { name: 'Connect Vercel', exact: true }).click();
    await expect(page.getByLabel('Vercel access token', { exact: false })).toHaveValue('');
    expect(fixture.unexpected).toEqual([]);
  });
}

test('cancel clears tokens; empty project selection denies sync; disconnect only calls Cooker with the saved version', async ({ page }) => {
  const fixture = await setup(page);
  await page.goto('/settings');
  await page.getByRole('button', { name: 'Connect Vercel', exact: true }).click();
  await page.getByLabel('Vercel access token', { exact: false }).fill(TOKEN);
  await page.getByRole('button', { name: 'Cancel connection', exact: true }).click();
  await page.getByRole('button', { name: 'Connect Vercel', exact: true }).click();
  await expect(page.getByLabel('Vercel access token', { exact: false })).toHaveValue('');
  await page.getByRole('button', { name: 'Cancel connection', exact: true }).click();
  await page.getByRole('button', { name: 'Manage projects & connection' }).click();
  await page.getByRole('checkbox', { name: 'Shop', exact: false }).uncheck();
  await page.getByLabel('Replacement Vercel token', { exact: false }).fill(TOKEN);
  await page.getByRole('button', { name: 'Save Vercel settings' }).click();
  await expect(page.getByRole('button', { name: 'Sync now', exact: true })).toBeDisabled();
  expect(fixture.writes[0]).toMatchObject({ method: 'PUT', body: { version: 3, token: TOKEN, projectIds: [] } });
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click();
  await page.getByRole('button', { name: 'Disconnect from Cooker?', exact: true }).click();
  await expect(page.getByText('Disconnected from Cooker. Your Vercel projects and token are unchanged.')).toBeVisible();
  expect(fixture.writes[1]).toEqual({ method: 'DELETE', version: '4' });
  expect(fixture.unexpected).toEqual([]);
});

test('discovery count and filtered pages do not silently select new projects', async ({ page }) => {
  const fixture = await setup(page, { discoveryProjects: [{ id: 'prj-shop', name: 'Shop' }, ...Array.from({ length: 60 }, (_, i) => ({ id: `prj-new-${i}`, name: `New project ${i.toString().padStart(2, '0')}` }))] });
  await page.goto('/settings');
  await page.getByRole('button', { name: 'Manage projects & connection' }).click();
  await expect(page.getByText('Current discovery: 61 accessible projects · 1 selected.', { exact: false })).toBeVisible();
  const projects = page.getByRole('group', { name: 'Projects to sync' });
  await expect(projects.getByRole('checkbox')).toHaveCount(25);
  await expect(projects.getByRole('checkbox', { name: 'Shop', exact: true })).toBeChecked();
  await page.getByRole('button', { name: 'Next project page' }).click();
  await expect(page.getByText('Page 2 of 3 · 61 matching projects', { exact: true })).toBeVisible();
  await expect(projects.getByRole('checkbox')).toHaveCount(25);
  await expect(projects.getByRole('checkbox', { name: 'New project 24', exact: true })).not.toBeChecked();
  await page.getByLabel('Find Vercel projects', { exact: true }).fill('New project 59');
  await expect(projects.getByRole('checkbox')).toHaveCount(1);
  await expect(page.getByText('Page 1 of 1 · 1 matching projects', { exact: true })).toBeVisible();
  await expect(projects.getByRole('checkbox')).not.toBeChecked();
  await projects.getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Save Vercel settings' }).click();
  expect(fixture.writes).toEqual([{ method: 'PUT', body: { version: 3, name: base.name, enabled: true, projectIds: ['prj-shop', 'prj-new-59'] } }]);
  expect(fixture.unexpected).toEqual([]);
});

test('last observed coverage stays labelled and failed discovery never claims an empty complete account', async ({ page }) => {
  const fixture = await setup(page, { discoveryUnavailable: true, coverage: { accessibleProjects: 3, selectedProjects: 1, observedAt: '2026-09-30T08:00:00Z' } });
  await page.goto('/cloud');
  await expect(page.getByText('Last discovery: 1 selected of 3 accessible projects', { exact: false })).toBeVisible();
  await expect(page.getByText('Coverage applies to this account or team and this connection’s permissions.', { exact: false })).toBeVisible();
  await page.goto('/settings');
  await page.getByRole('button', { name: 'Manage projects & connection' }).click();
  await expect(page.getByText('Discovery could not verify complete project coverage', { exact: true })).toBeVisible();
  await expect(page.getByText('Current discovery:', { exact: false })).toHaveCount(0);
  await expect(page.getByText('Last discovery: 1 selected of 3 accessible projects', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Save Vercel settings' }).click();
  expect(fixture.writes).toEqual([{ method: 'PUT', body: { version: 3, name: base.name, enabled: true } }]);
});

test('a conflicting edit remains unsaved and never reports success', async ({ page }) => {
  const fixture = await setup(page); fixture.conflict();
  await page.goto('/settings');
  await page.getByRole('button', { name: 'Manage projects & connection' }).click();
  await page.getByRole('checkbox', { name: 'API', exact: true }).check();
  await page.getByRole('button', { name: 'Save Vercel settings' }).click();
  await expect(page.getByText('Connection changed. Reload before saving.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save Vercel settings' })).toBeVisible();
  await expect(page.getByText('Vercel settings saved.', { exact: false })).toHaveCount(0);
});

test('Vercel history stays available with inventory off and exposes only safe deployment links', async ({ page }) => {
  const fixture = await setup(page);
  await page.goto('/cloud');
  await expect(page.getByRole('heading', { name: 'Cloud inventory is off' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Vercel deployments' })).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
  const list = page.getByRole('list', { name: 'Vercel deployment history' });
  await expect(list.getByRole('listitem')).toHaveCount(2);
  await expect(list.getByRole('link', { name: 'Open deployment' })).toHaveAttribute('href', 'https://shop.example.test/');
  await expect(list.getByRole('link')).toHaveCount(1);
  await expect(list.getByText('Failed', { exact: true })).toBeVisible();
  await expect(page.getByText('Out of date', { exact: true })).toBeVisible();
  await page.getByRole('combobox', { name: 'Vercel project', exact: true }).selectOption(`${base.id}:prj-shop`);
  await expect(list.getByRole('listitem')).toHaveCount(1);
  await page.getByRole('button', { name: 'Sync now', exact: true }).click();
  await expect(page.getByText('Up to date', { exact: true })).toBeVisible();
  expect(fixture.writes).toEqual([{ method: 'SYNC' }]);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('refreshes the snapshot automatically and preserves visible last-good history when refresh or sync fails', async ({ page }) => {
  const fixture = await setup(page);
  await page.clock.install();
  await page.goto('/cloud');
  const list = page.getByRole('list', { name: 'Vercel deployment history' });
  await expect(list.getByRole('listitem')).toHaveCount(2);
  const count = fixture.snapshotReads();
  await page.clock.fastForward(30_000);
  await expect.poll(fixture.snapshotReads).toBeGreaterThan(count);
  fixture.failSync();
  await page.getByRole('button', { name: 'Sync now', exact: true }).click();
  await expect(page.getByText('Sync failed', { exact: true })).toBeVisible();
  await expect(list.getByRole('listitem')).toHaveCount(2);
  fixture.failSnapshots();
  await page.clock.fastForward(30_000);
  await expect(page.getByText('Vercel snapshot temporarily unavailable', { exact: false })).toBeVisible();
  await expect(list.getByRole('listitem')).toHaveCount(2);
  await expect(page.getByRole('button', { name: 'Retry Vercel history' })).toBeVisible();
});

test('viewers can read deployment history without connection or sync controls', async ({ page }) => {
  const fixture = await setup(page, { viewer: true });
  await page.goto('/cloud');
  await expect(page.getByRole('list', { name: 'Vercel deployment history' }).getByRole('listitem')).toHaveCount(2);
  await expect(page.getByRole('button', { name: 'Sync now', exact: true })).toHaveCount(0);
  await page.goto('/settings');
  await expect(page.getByText('Ask an administrator to connect Vercel.', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Connect Vercel', exact: true })).toHaveCount(0);
  expect(fixture.writes).toEqual([]);
});

test('Vercel panels retain readable desktop/mobile layouts and accessible controls', async ({ page }, testInfo) => {
  await setup(page);
  await page.goto('/cloud');
  await expect(page.getByRole('list', { name: 'Vercel deployment history' }).getByRole('listitem')).toHaveCount(2);
  const cloud = page.locator('section.panel').filter({ has: page.getByRole('heading', { name: 'Vercel deployments', exact: true }) });
  await cloud.screenshot({ path: testInfo.outputPath('vercel-cloud-desktop.png') });
  expect((await new AxeBuilder({ page }).include('main').analyze()).violations).toEqual([]);
  await page.goto('/settings');
  await page.getByRole('button', { name: 'Manage projects & connection' }).click();
  await expect(page.getByRole('checkbox', { name: 'Shop', exact: false })).toBeVisible();
  await page.locator('#vercel-connections').screenshot({ path: testInfo.outputPath('vercel-settings-desktop.png') });
  expect((await new AxeBuilder({ page }).include('#vercel-connections').analyze()).violations).toEqual([]);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator('#vercel-connections').screenshot({ path: testInfo.outputPath('vercel-settings-mobile.png') });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const save = page.getByRole('button', { name: 'Save Vercel settings', exact: true });
  await save.scrollIntoViewIfNeeded();
  await expect(save).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath('vercel-settings-mobile-actions.png') });
  await save.click();
  await expect(page.getByText('Vercel settings saved.', { exact: false })).toBeVisible();
});
