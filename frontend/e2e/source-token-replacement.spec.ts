import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi } from './fixtures/api';
import type { SourceConnection } from '../src/api/automation';

// Presentation/roles/request contracts only; all credentials and sessions are
// synthetic. No provider, owner account or actual backend participates.
const TOKEN = 'synthetic-replacement-browser-token';
const CANARY = 'synthetic-private-provider-error';
const connection: SourceConnection = {
  id: 'owned-source', name: 'Owned Git', provider: 'gitea', origin: 'https://git.example.test',
  apiOrigin: 'https://git.example.test', enabled: true, version: 4, repositories: ['team/one', 'team/two'],
  authMode: 'token', username: 'owned-user', webhookAuthMode: 'signing', hasToken: true, hasWebhookSecret: true,
};

async function setup(page: Page, options: { role?: string; provider?: SourceConnection['provider']; outcome?: string; theme?: string } = {}) {
  await mockApi(page);
  await page.addInitScript(({ theme }) => {
    localStorage.setItem('cooker.local.token', 'synthetic-source-session');
    localStorage.setItem('cooker-ui', JSON.stringify({ state: { mode: 'pro', themeMode: theme, calmMode: true }, version: 0 }));
  }, { theme: options.theme ?? 'dark' });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: 'owned-source-user', email: 'fixture@invalid.example', name: 'Fixture', role: options.role ?? 'admin', roles: [options.role ?? 'admin'] } }));
  let saved = { ...structuredClone(connection), provider: options.provider ?? 'gitea' };
  const writes: Record<string, unknown>[] = []; const reads: string[] = []; const errors: string[] = [];
  let release: (() => void) | undefined; let invalidReload = false;
  const pending = new Promise<void>((resolve) => { release = resolve; });
  page.on('pageerror', (error) => errors.push(error.message));
  await page.route('**/api/v1/admin/source-connections', (route) => {
    reads.push(route.request().method());
    return route.fulfill({ json: invalidReload ? null : [saved] });
  });
  await page.route('**/api/v1/admin/source-connections/owned-source/token-replacement', async (route) => {
    writes.push(route.request().postDataJSON());
    if (options.outcome === 'denied') return route.fulfill({ status: 422, json: { error: CANARY } });
    if (options.outcome === 'conflict') { saved = { ...saved, name: 'Concurrent source edit', version: 5 }; return route.fulfill({ status: 409, json: { error: CANARY } }); }
    saved = { ...saved, version: 5 };
    if (options.outcome === 'lost-response') return route.fulfill({ status: 503, json: { error: CANARY } });
    if (options.outcome === 'network') return route.abort('failed');
    if (options.outcome === 'pending') await pending;
    const metadata = options.outcome === 'malformed' ? { ...saved, id: 'other-source' } : options.outcome === 'incomplete' ? { id: saved.id, version: saved.version } : saved;
    return route.fulfill({ json: { connection: metadata, checkedRepositories: 2, check: 'repository_metadata_access' } });
  });
  return { writes, reads, errors, release: () => release?.(), failReload: () => { invalidReload = true; } };
}

async function openEditor(page: Page) {
  await page.goto('/settings');
  await page.getByRole('button', { name: 'Replace repository token', exact: true }).click();
  const form = page.getByRole('form', { name: 'Replace token for Owned Git', exact: true });
  await expect(form.getByRole('textbox', { name: 'Replacement repository token', exact: false })).toHaveValue('');
  return form;
}

async function assertPrivateDraftNotStored(page: Page) {
  const storage = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }));
  expect(storage).not.toContain(TOKEN); expect(storage).not.toContain(CANARY);
}

for (const provider of ['github', 'gitlab', 'gitea'] as const) test(`${provider} administrator checks a versioned replacement without exposing the saved token`, async ({ page }) => {
  const f = await setup(page, { provider }); const form = await openEditor(page);
  const token = form.getByRole('textbox', { name: 'Replacement repository token', exact: false });
  await expect(token).toHaveAttribute('type', 'password'); await expect(token).toHaveAttribute('autocomplete', 'new-password');
  await token.fill(TOKEN); await assertPrivateDraftNotStored(page);
  await form.getByRole('button', { name: 'Check and replace token', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Token checked for 2 allowed repositories and replaced.' })).toBeVisible();
  expect(f.writes).toEqual([{ version: 4, token: TOKEN, username: 'owned-user' }]);
  await expect(page.getByRole('form', { name: /Replace token for/ })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText(TOKEN); await assertPrivateDraftNotStored(page);
  expect(f.errors).toEqual([]);
});

test('failed access stays safely editable without echoing provider errors', async ({ page }) => {
  const f = await setup(page, { outcome: 'denied' }); const form = await openEditor(page);
  await form.getByRole('textbox', { name: 'Replacement repository token', exact: false }).fill(TOKEN);
  await form.getByRole('button', { name: 'Check and replace token', exact: true }).click();
  await expect(form.getByRole('alert')).toContainText('replacement was not saved');
  await expect(form.getByRole('textbox', { name: 'Replacement repository token', exact: false })).toHaveValue(TOKEN);
  await expect(form.getByRole('button', { name: 'Check and replace token', exact: true })).toBeEnabled();
  await expect(page.locator('body')).not.toContainText(CANARY); await assertPrivateDraftNotStored(page);
  await form.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Replace repository token', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Replacement repository token', exact: false })).toHaveValue('');
  expect(f.writes).toHaveLength(1); expect(f.errors).toEqual([]);
});

for (const outcome of ['conflict', 'lost-response', 'network', 'malformed', 'incomplete']) test(`${outcome} requires explicit reload and never automatically repeats the write`, async ({ page }) => {
  const f = await setup(page, { outcome }); const form = await openEditor(page);
  await form.getByRole('textbox', { name: 'Replacement repository token', exact: false }).fill(TOKEN);
  await form.getByRole('button', { name: 'Check and replace token', exact: true }).click();
  await expect(form.getByRole('button', { name: 'Reload connection and clear draft', exact: true })).toBeVisible();
  await expect(form.getByRole('textbox', { name: 'Replacement repository token', exact: false })).toBeDisabled();
  await expect(form.getByRole('button', { name: 'Check and replace token', exact: true })).toHaveCount(0);
  if (outcome !== 'conflict') { await expect(form.getByRole('alert')).toContainText('could not be confirmed'); await expect(form).not.toContainText('saved token has not changed'); }
  await expect(page.locator('body')).not.toContainText(CANARY); await assertPrivateDraftNotStored(page);
  await form.getByRole('button', { name: 'Reload connection and clear draft', exact: true }).click();
  await expect.poll(() => f.reads.length).toBeGreaterThan(1);
  const replace = page.getByRole('button', { name: 'Replace repository token', exact: true });
  await expect(replace).toBeVisible(); await replace.click();
  await expect(page.getByRole('textbox', { name: 'Replacement repository token', exact: false })).toHaveValue('');
  expect(f.writes).toHaveLength(1); expect(f.errors).toEqual([]);
});

test('malformed reload metadata retains the uncertain draft and cannot authorize another replacement', async ({ page }) => {
  const f = await setup(page, { outcome: 'lost-response' }); const form = await openEditor(page);
  await form.getByRole('textbox', { name: 'Replacement repository token', exact: false }).fill(TOKEN);
  await form.getByRole('button', { name: 'Check and replace token', exact: true }).click();
  await expect(form.getByRole('button', { name: 'Reload connection and clear draft', exact: true })).toBeVisible(); f.failReload();
  await form.getByRole('button', { name: 'Reload connection and clear draft', exact: true }).click();
  await expect(form.getByRole('alert')).toContainText('Could not reload this connection');
  await expect(form.getByRole('textbox', { name: 'Replacement repository token', exact: false })).toHaveValue(TOKEN);
  await expect(form.getByRole('textbox', { name: 'Replacement repository token', exact: false })).toBeDisabled();
  expect(f.writes).toHaveLength(1); expect(f.errors).toEqual([]);
});

for (const role of ['viewer', 'operator']) test(`${role} cannot read or replace source credentials in the workspace`, async ({ page }) => {
  const f = await setup(page, { role }); await page.goto('/settings');
  await expect(page.getByText('Ask an administrator to connect a Git provider.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Replace repository token', exact: true })).toHaveCount(0);
  expect(f.reads).toEqual([]); expect(f.writes).toEqual([]); expect(f.errors).toEqual([]);
});

test('departed editor cannot clear a new page draft when its old request completes', async ({ page }) => {
  const f = await setup(page, { outcome: 'pending' }); const form = await openEditor(page);
  await form.getByRole('textbox', { name: 'Replacement repository token', exact: false }).fill(TOKEN);
  await form.getByRole('button', { name: 'Check and replace token', exact: true }).click();
  await expect.poll(() => f.writes.length).toBe(1);
  await page.getByRole('link', { name: 'Apps', exact: true }).first().click();
  await page.getByRole('link', { name: 'Settings', exact: true }).first().click();
  await page.getByRole('button', { name: 'Replace repository token', exact: true }).click();
  const newer = page.getByRole('textbox', { name: 'Replacement repository token', exact: false }); await newer.fill('synthetic-newer-draft');
  f.release();
  await expect(newer).toHaveValue('synthetic-newer-draft');
  await expect(page.getByRole('status').filter({ hasText: 'Token checked for' })).toHaveCount(0);
  expect(f.writes).toHaveLength(1); await assertPrivateDraftNotStored(page); expect(f.errors).toEqual([]);
});

test('sign-out clears the replacement draft and prevents a delayed acknowledgement from restoring it', async ({ page }) => {
  const f = await setup(page, { outcome: 'pending' }); const form = await openEditor(page);
  await form.getByRole('textbox', { name: 'Replacement repository token', exact: false }).fill(TOKEN);
  await form.getByRole('button', { name: 'Check and replace token', exact: true }).click(); await expect.poll(() => f.writes.length).toBe(1);
  await page.getByRole('button', { name: 'Sign out', exact: true }).click(); await expect(page).toHaveURL(/\/signin/);
  f.release(); await expect(page.getByRole('form', { name: /Replace token for/ })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText(TOKEN); await assertPrivateDraftNotStored(page); expect(f.writes).toHaveLength(1); expect(f.errors).toEqual([]);
});

for (const theme of ['dark', 'light']) test(`${theme} token editor supports narrow keyboard entry and accessible controls`, async ({ page }) => {
  const f = await setup(page, { theme }); await page.setViewportSize({ width: 390, height: 844 }); const form = await openEditor(page);
  const input = form.getByRole('textbox', { name: 'Replacement repository token', exact: false }); await input.focus(); await page.keyboard.type(TOKEN);
  await expect(input).toHaveValue(TOKEN); await expect(form.getByRole('button', { name: 'Check and replace token', exact: true })).toBeEnabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).include('form[aria-label="Replace token for Owned Git"]').analyze()).violations).toEqual([]);
  await assertPrivateDraftNotStored(page); expect(f.writes).toEqual([]); expect(f.errors).toEqual([]);
});
