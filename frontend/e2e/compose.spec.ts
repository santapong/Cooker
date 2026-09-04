import { expect, test } from '@playwright/test';
import { mockApi, mockCompose } from './fixtures/api';
import { settleEntrances } from './fixtures/motion';

async function openCompose(page: import('@playwright/test').Page, updateStatus?: number) {
  await mockApi(page);
  await mockCompose(page, { updateStatus });
  await page.goto('/docker/compose');
  await expect(page.locator('.star')).toHaveCount(3);
  await expect.poll(() => page.locator('.react-flow__edges > *').count()).toBeGreaterThan(0);
  await settleEntrances(page);
}

test.describe('compose porthole — service editor', () => {
  test('editing a service sends the patch, mirrors it into the scene and confirms with a toast', async ({ page }, testInfo) => {
    await openCompose(page);
    // links: two plain depends_on lines and one dashed env reference named by its variable
    await expect(page.locator('.constellation-cond')).toHaveText(['API_URL']);
    await expect(page.locator('.constellation.cond-always')).toHaveCount(1);
    await page.locator('.star', { hasText: 'api' }).click();
    const inspector = page.getByRole('complementary', { name: 'Service api' });
    await expect(inspector).toBeVisible();
    await expect(inspector.getByLabel('Image')).toHaveValue('ghcr.io/acme/api:2.1.0');
    await expect(inspector.getByLabel('Environment')).toHaveValue('DATABASE_URL=postgres://db/acme\nLOG_LEVEL=info');
    await expect(inspector.getByText('mem 512m · cpu 0.5')).toBeVisible();
    await expect(inspector.getByRole('button', { name: 'Save' })).toBeDisabled();

    await inspector.getByLabel('Image').fill('ghcr.io/acme/api:2.2.0');
    await inspector.getByLabel('Ports').fill('9000:9000\n9443:9443');
    await inspector.getByLabel('Environment').fill('DATABASE_URL=postgres://db/acme\nLOG_LEVEL=debug');
    await expect(inspector.getByText('edited')).toBeVisible();

    const put = page.waitForRequest((r) => r.method() === 'PUT' && r.url().includes('/docker/compose/services/api'));
    await inspector.getByRole('button', { name: 'Save' }).click();
    expect((await put).postDataJSON()).toEqual({
      composePath: 'docker-compose.yml',
      image: 'ghcr.io/acme/api:2.2.0',
      ports: ['9000:9000', '9443:9443'],
      environment: { DATABASE_URL: 'postgres://db/acme', LOG_LEVEL: 'debug' },
    });

    const toast = page.locator('.toast[data-kind="success"]');
    await expect(toast).toContainText('api: Service config updated.');
    await expect(toast).toHaveAttribute('role', 'status');
    // the scene mirrors the patch: the star's sub-label is the image
    await expect(page.locator('.star', { hasText: 'api' })).toContainText('ghcr.io/acme/api:2.2.0');
    await expect(inspector.getByRole('button', { name: 'Save' })).toBeDisabled();
    await page.screenshot({ path: testInfo.outputPath('compose-editor.png') });
    await testInfo.attach('compose-editor', { path: testInfo.outputPath('compose-editor.png'), contentType: 'image/png' });

    await toast.getByRole('button', { name: 'Dismiss notification' }).click();
    await expect(page.locator('.toast')).toHaveCount(0);
  });

  test('a rejected patch stays in the inspector as an inline error and a malformed env line never leaves the browser', async ({ page }) => {
    await openCompose(page, 500);
    await page.locator('.star', { hasText: 'db' }).click();
    const inspector = page.getByRole('complementary', { name: 'Service db' });

    await inspector.getByLabel('Environment').fill('POSTGRES_DB=acme\n2BAD=x');
    await inspector.getByRole('button', { name: 'Save' }).click();
    await expect(inspector.getByRole('alert')).toContainText('Line 2: "2BAD" is not a valid variable name.');

    await inspector.getByLabel('Environment').fill('POSTGRES_DB=acme\nPGDATA=/data');
    await inspector.getByRole('button', { name: 'Save' }).click();
    await expect(inspector.getByRole('alert')).toContainText('cannot update db');
    await expect(page.locator('.toast')).toHaveCount(0);
    await expect(inspector.getByLabel('Environment')).toHaveValue('POSTGRES_DB=acme\nPGDATA=/data');
  });

  test('toast entrance rises under full motion and only fades under reduced motion', async ({ page }) => {
    await openCompose(page);
    await page.locator('.star', { hasText: 'web' }).click();
    const inspector = page.getByRole('complementary', { name: 'Service web' });
    await inspector.getByLabel('Image').fill('ghcr.io/acme/web:1.5.0');
    await inspector.getByRole('button', { name: 'Save' }).click();
    const toast = page.locator('.toast').first();
    await expect(toast).toBeVisible();
    expect(await toast.evaluate((el) => getComputedStyle(el).animationName)).toBe('toast-in');
    await expect(toast).toHaveAttribute('role', 'status');

    await page.emulateMedia({ reducedMotion: 'reduce' });
    expect(await toast.evaluate((el) => getComputedStyle(el).animationName)).toBe('toast-fade');
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.evaluate(() => document.documentElement.setAttribute('data-calm', 'true'));
    expect(await toast.evaluate((el) => getComputedStyle(el).animationName)).toBe('toast-fade');
  });
});
