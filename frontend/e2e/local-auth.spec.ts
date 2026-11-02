import { expect, test, type Page } from '@playwright/test';
import { mockApi } from './fixtures/api';

// Run only with playwright.local-auth.config.ts: it builds an actual local-only
// authentication bundle. API replies use dummy fixture identities; this is not
// backend password verification or an operator account sign-in.
const user = { id: 'local-fixture', name: 'Local fixture user', email: 'local@cooker.test', role: 'admin', roles: ['admin'] };
const token = 'fixture-local-auth-token';

async function setup(page: Page, stored?: 'valid' | 'invalid') {
  await mockApi(page);
  let protectedWithoutToken = 0;
  let identityChecks = 0;
  let signIns = 0;
  if (stored) await page.addInitScript(({ value }) => {
    // Seed once so reload/logout assertions exercise the real saved state.
    if (!sessionStorage.getItem('fixture-seeded')) {
      localStorage.setItem('cooker.local.token', value);
      sessionStorage.setItem('fixture-seeded', 'true');
    }
  }, { value: stored === 'valid' ? token : 'fixture-expired-token' });
  await page.route('**/api/v1/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/api/v1/auth/methods') return route.fulfill({ json: { oidc: { enabled: false }, local: { enabled: true, allowSignup: false } } });
    if (path === '/api/v1/auth/local/me') {
      identityChecks++;
      return request.headers().authorization === `Bearer ${token}`
        ? route.fulfill({ json: user })
        : route.fulfill({ status: 401, json: { error: 'Fixture token expired' } });
    }
    if (path === '/api/v1/auth/local/signin') {
      signIns++;
      return route.fulfill({ json: { token, user, expiresAt: '2099-01-01T00:00:00Z' } });
    }
    if (request.headers().authorization !== `Bearer ${token}`) {
      protectedWithoutToken++;
      return route.fulfill({ status: 401, json: { error: 'Authentication required' } });
    }
    return route.fallback();
  });
  await page.route('**/api/v1/admin/source-connections', (route) => route.fulfill({ json: [] }));
  return { protectedWithoutToken: () => protectedWithoutToken, identityChecks: () => identityChecks, signIns: () => signIns };
}

for (const path of ['/signin', '/settings']) {
  test(`local-only build without a stored token stays at sign-in from ${path}`, async ({ page }) => {
    const fixture = await setup(page);
    await page.clock.install();
    await page.goto(path);
    await expect(page).toHaveURL(/\/signin$/);
    await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible();
    await expect(page.getByLabel('Email', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Continue with single sign-on' })).toHaveCount(0);
    await page.clock.fastForward(15_000);
    await expect(page).toHaveURL(/\/signin$/);
    await expect(page.getByLabel('Password', { exact: true })).toBeVisible();
    expect(fixture.protectedWithoutToken()).toBe(0);
    expect(fixture.identityChecks()).toBe(0);
    expect(await page.evaluate(() => localStorage.getItem('cooker.local.token'))).toBeNull();
  });
}

test('restores a valid saved local session without requiring OIDC configuration', async ({ page }) => {
  const fixture = await setup(page, 'valid');
  await page.goto('/settings');
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
  await expect(page.getByText('Local fixture user', { exact: true })).toBeVisible();
  expect(fixture.identityChecks()).toBe(1);
  expect(fixture.protectedWithoutToken()).toBe(0);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
  expect(fixture.identityChecks()).toBe(2);
});

test('clears an expired local token and remains signed out after reload', async ({ page }) => {
  const fixture = await setup(page, 'invalid');
  await page.goto('/settings');
  await expect(page.getByLabel('Email', { exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/signin$/);
  expect(await page.evaluate(() => localStorage.getItem('cooker.local.token'))).toBeNull();
  await page.reload();
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/signin$/);
  expect(fixture.identityChecks()).toBe(1);
  expect(fixture.protectedWithoutToken()).toBe(0);
});

test('local sign-in returns to the requested page and sign-out remains signed out', async ({ page }) => {
  const fixture = await setup(page);
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/signin$/);
  await page.getByLabel('Email', { exact: true }).fill('local@cooker.test');
  await page.getByLabel('Password', { exact: true }).fill('dummy-browser-fixture-password');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible();
  expect(fixture.signIns()).toBe(1);
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page).toHaveURL(/\/signin$/);
  await expect(page.getByLabel('Email', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem('cooker.local.token'))).toBeNull();
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/signin$/);
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible();
  expect(fixture.protectedWithoutToken()).toBe(0);
});

test('password visibility and access help never submit credentials', async ({ page }) => {
  const fixture = await setup(page);
  await page.goto('/signin');
  const password = page.getByLabel('Password', { exact: true });
  await password.fill('dummy-visibility-fixture');
  await expect(password).toHaveAttribute('type', 'password');
  await page.getByRole('button', { name: 'Show password', exact: true }).click();
  await expect(password).toHaveAttribute('type', 'text');
  await expect(password).toHaveValue('dummy-visibility-fixture');
  await page.getByRole('button', { name: 'Hide password', exact: true }).click();
  await expect(password).toHaveAttribute('type', 'password');
  await page.getByText('Need help signing in?', { exact: true }).click();
  await expect(page.getByText(/contact the administrator of this Cooker instance/)).toBeVisible();
  expect(fixture.signIns()).toBe(0);
  expect(await page.evaluate(() => localStorage.getItem('cooker.local.token'))).toBeNull();
});

test('a rejected password preserves form values and offers a real retry', async ({ page }) => {
  await setup(page);
  await page.route('**/api/v1/auth/local/signin', (route) => route.fulfill({ status: 401, json: { error: 'Email or password is incorrect.' } }));
  await page.goto('/signin');
  await page.getByLabel('Email', { exact: true }).fill('local@cooker.test');
  await page.getByLabel('Password', { exact: true }).fill('dummy-rejected-fixture');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Email or password is incorrect.');
  await expect(page.getByLabel('Email', { exact: true })).toHaveValue('local@cooker.test');
  await expect(page.getByLabel('Password', { exact: true })).toHaveValue('dummy-rejected-fixture');
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeEnabled();
  await expect(page).toHaveURL(/\/signin$/);
});

test('discovery network failure has an honest retry instead of an invented provider', async ({ page }) => {
  await setup(page);
  let available = false;
  await page.route('**/api/v1/auth/methods', (route) => available
    ? route.fulfill({ json: { oidc: { enabled: false }, local: { enabled: true, allowSignup: false } } })
    : route.abort('failed'));
  await page.goto('/signin');
  await expect(page.getByRole('alert')).toContainText('could not check');
  await expect(page.getByRole('button', { name: 'Continue with single sign-on' })).toHaveCount(0);
  await expect(page.getByLabel('Email', { exact: true })).toHaveCount(0);
  available = true;
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(page.getByLabel('Email', { exact: true })).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('closed signup does not render or submit a local signup form', async ({ page }) => {
  await setup(page);
  let requests = 0;
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/v1/auth/local/signup') requests++;
  });
  await page.goto('/signup');
  await expect(page.getByRole('heading', { name: 'Create an account', exact: true })).toBeVisible();
  await expect(page.getByRole('status')).toContainText(/sign-up is not open/i);
  await expect(page.getByLabel('Email', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Create account', exact: true })).toHaveCount(0);
  await page.getByRole('link', { name: 'Already have an account? Sign in' }).click();
  await expect(page).toHaveURL(/\/signin$/);
  expect(requests).toBe(0);
});

test('signup requires matching long passwords and sends the actual entered account', async ({ page }) => {
  await setup(page);
  await page.route('**/api/v1/auth/methods', (route) => route.fulfill({ json: { oidc: { enabled: false }, local: { enabled: true, allowSignup: true } } }));
  let submitted: unknown;
  await page.route('**/api/v1/auth/local/signup', (route) => {
    submitted = route.request().postDataJSON();
    return route.fulfill({ status: 409, json: { error: 'Fixture account already exists.' } });
  });
  await page.goto('/signup');
  await page.getByLabel('Name (optional)', { exact: true }).fill('New fixture');
  await page.getByLabel('Email', { exact: true }).fill('new@cooker.test');
  const password = page.getByLabel('Password', { exact: true });
  const confirm = page.getByLabel('Confirm password', { exact: true });
  const create = page.getByRole('button', { name: 'Create account', exact: true });
  await password.fill('short');
  await confirm.fill('short');
  await expect(create).toBeDisabled();
  await password.fill('dummy-signup-password');
  await confirm.fill('dummy-other-password');
  await expect(page.getByText('Passwords do not match.', { exact: true })).toBeVisible();
  await expect(create).toBeDisabled();
  await confirm.fill('dummy-signup-password');
  await page.getByRole('button', { name: 'Show confirm password', exact: true }).click();
  await expect(confirm).toHaveAttribute('type', 'text');
  await expect(password).toHaveAttribute('type', 'password');
  await expect(create).toBeEnabled();
  await create.click();
  await expect(page.getByRole('alert')).toContainText('Fixture account already exists.');
  expect(submitted).toEqual({ name: 'New fixture', email: 'new@cooker.test', password: 'dummy-signup-password' });
  await expect(confirm).toHaveValue('dummy-signup-password');
});

test('Split login keeps the real form usable on narrow screens and reduced motion', async ({ page }) => {
  await setup(page);
  await page.setViewportSize({ width: 320, height: 740 });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/signin');
  await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible();
  await expect(page.locator('.auth-story')).toBeHidden();
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(await page.locator('.airlock-card').evaluate((el) => getComputedStyle(el).animationName)).toBe('airlock-fade');
  const inputBox = await page.getByLabel('Password', { exact: true }).boundingBox();
  expect(inputBox?.height).toBeGreaterThanOrEqual(44);
});
