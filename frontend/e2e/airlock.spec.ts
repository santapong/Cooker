import { expect, test } from '@playwright/test';
import { mockApi, mockSignedOut } from './fixtures/api';

test.describe('airlock — signed-out session', () => {
  test.beforeEach(async ({ page }) => {
    await mockApi(page);
    await mockSignedOut(page);
  });

  test('sign-in offers both methods on the card and links to sign-up', async ({ page }, testInfo) => {
    await page.goto('/signin');
    await expect(page).toHaveURL(/\/signin$/);
    const card = page.locator('.airlock-card');
    await expect(card.getByRole('heading', { level: 1, name: 'Sign in' })).toBeVisible();
    await expect(card.getByRole('button', { name: 'Continue with single sign-on' })).toBeVisible();
    await expect(card.getByLabel('Email')).toBeVisible();
    await expect(card.getByLabel('Password')).toBeVisible();
    await expect(card.getByRole('button', { name: 'Sign in' })).toBeVisible();
    await expect(card.getByRole('link', { name: 'Create an account' })).toHaveAttribute('href', '/signup');
    await expect(page.locator('.airlock .starfield')).toHaveCount(1);
    await page.screenshot({ path: testInfo.outputPath('signin.png') });
    await testInfo.attach('signin', { path: testInfo.outputPath('signin.png'), contentType: 'image/png' });
  });

  test('sign-up renders the account form and the card rises only under full motion', async ({ page }, testInfo) => {
    // A direct /signup visit boots as the dev user and bounces to /signin; take the real path via the link.
    await page.goto('/signin');
    await page.locator('.airlock-card').getByRole('link', { name: 'Create an account' }).click();
    await expect(page).toHaveURL(/\/signup$/);
    const card = page.locator('.airlock-card');
    await expect(card.getByRole('heading', { level: 1, name: 'Create an account' })).toBeVisible();
    await expect(card.getByLabel('Confirm password')).toBeVisible();
    expect(await card.evaluate((el) => getComputedStyle(el).animationName)).toBe('airlock-in');
    await page.screenshot({ path: testInfo.outputPath('signup.png') });
    await testInfo.attach('signup', { path: testInfo.outputPath('signup.png'), contentType: 'image/png' });

    await page.emulateMedia({ reducedMotion: 'reduce' });
    expect(await card.evaluate((el) => getComputedStyle(el).animationName)).toBe('airlock-fade');
  });

  test('a protected route bounces to the airlock and returns after sign-in', async ({ page }) => {
    await page.route('**/api/v1/auth/local/signin', (route) =>
      route.fulfill({ json: { token: 'e2e-fresh', expiresAt: '2099-01-01T00:00:00Z', user: { id: 'u1', email: 'e2e@cooker.local', name: 'E2E', role: 'admin', roles: ['admin'] } } }),
    );
    await page.goto('/pipelines');
    await expect(page).toHaveURL(/\/signin$/);
    await page.getByLabel('Email').fill('e2e@cooker.local');
    await page.getByLabel('Password').fill('correct horse');
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page).toHaveURL(/\/pipelines$/);
    await expect(page.locator('a.chart-name').first()).toBeVisible();
  });
});
