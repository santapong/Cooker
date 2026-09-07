import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mockApi, mockCompose, TYPED_PIPELINE } from './fixtures/api';
import { settleEntrances } from './fixtures/motion';

test('all six stage types remain identifiable in editor and run views', async ({ page }, testInfo) => {
  await mockApi(page);
  await page.route('**/api/v1/pipelines/gates', (route) => route.fulfill({ json: TYPED_PIPELINE }));
  for (const route of ['/pipelines/gates/edit', '/pipelines/gates/runs/r1']) {
    await page.goto(route);
    await expect(page.locator('.stage-symbol')).not.toHaveCount(0);
    for (const stage of TYPED_PIPELINE.stages) {
      const node = page.locator('.star[data-stage-type="' + stage.type + '"]');
      await expect(node.locator('.stage-kind')).toHaveText(new RegExp(stage.type, 'i'));
      await expect(node.locator('.lbl')).toHaveText(stage.name);
      await expect(node.locator('.stage-symbol')).toBeVisible();
    }
    expect(new Set(await page.locator('.star .stage-symbol').evaluateAll((icons) => icons.map((icon) => icon.innerHTML))).size).toBe(6);
    await expect.poll(() => page.locator('.react-flow__edges > *').count()).toBeGreaterThan(0);
    await settleEntrances(page);
    await page.screenshot({ path: testInfo.outputPath(route.endsWith('edit') ? 'typed-editor.png' : 'typed-run.png') });
  }
});

test('saved stacks survive reload, search, reopen the right file and keep errors visible', async ({ page }, testInfo) => {
  await mockApi(page); await mockCompose(page);
  await page.goto('/docker/compose');
  await expect(page.locator('.star')).toHaveCount(3);
  const library = page.getByRole('complementary', { name: 'Compose registry', exact: true });
  await library.getByLabel('Stack name').fill('Development');
  await library.getByRole('button', { name: 'Save stack', exact: true }).click();
  await page.getByLabel('Compose file path').fill('staging.yml');
  await expect(page.locator('.compose-map-file')).toHaveText('docker-compose.yml');
  await page.getByRole('button', { name: 'Parse', exact: true }).click();
  await expect(page.locator('.compose-map-file')).toHaveText('staging.yml');
  await library.getByLabel('Stack name').fill('Staging');
  await library.getByRole('button', { name: 'Save stack', exact: true }).click();
  await page.reload();
  await expect(library.getByRole('button', { name: 'Open Staging', exact: true })).toBeVisible();
  await library.getByLabel('Search saved stacks').fill('stag');
  await expect(library.getByRole('button', { name: 'Open Development', exact: true })).toHaveCount(0);
  const reopened = page.waitForRequest((r) => r.url().endsWith('/compose/parse') && r.postDataJSON()?.composePath === 'staging.yml');
  await library.getByRole('button', { name: 'Open Staging', exact: true }).click();
  await reopened;
  await expect(page.locator('.compose-map-file')).toHaveText('staging.yml');
  await settleEntrances(page);
  await page.locator('.star[data-stage-type="service"]').filter({ hasText: 'api' }).click();
  const inspector = page.getByRole('complementary', { name: 'Service api', exact: true });
  await inspector.getByLabel('Image').fill('acme/api:updated');
  const put = page.waitForRequest((r) => r.method() === 'PUT');
  await inspector.getByRole('button', { name: 'Save', exact: true }).click();
  expect((await put).postDataJSON().composePath).toBe('staging.yml');
  await expect(inspector.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await inspector.getByRole('button', { name: 'Close inspector' }).click();
  await page.route('**/api/v1/docker/compose/parse', (route) => route.fulfill({ status: 400, json: { error: 'file missing' } }));
  await page.getByLabel('Compose file path').fill('missing.yml');
  await page.getByRole('button', { name: 'Parse', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Still showing staging.yml');
  await expect(page.locator('.compose-map-file')).toHaveText('staging.yml');
  const axe = await new AxeBuilder({ page }).analyze();
  expect(axe.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')).toEqual([]);
  await page.screenshot({ path: testInfo.outputPath('compose-library.png') });
  await page.setViewportSize({ width: 760, height: 960 });
  await expect(library.getByRole('button', { name: 'Open Staging', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const libraryBox = await library.boundingBox();
  const cardBox = await library.locator('.compose-stack').boundingBox();
  const mainBox = await page.locator('.compose-main').boundingBox();
  expect(cardBox!.height).toBeGreaterThan(100);
  expect(cardBox!.y + cardBox!.height).toBeLessThanOrEqual(libraryBox!.y + libraryBox!.height);
  expect(mainBox!.y).toBeGreaterThanOrEqual(libraryBox!.y + libraryBox!.height);
  await page.locator('.compose-porthole').scrollIntoViewIfNeeded();
  await page.getByRole('button', { name: 'Fit map', exact: true }).click();
  const canvasBox = await page.locator('.run-canvas').boundingBox();
  expect(canvasBox!.height).toBeGreaterThan(200);
  await expect.poll(async () => {
    const frame = await page.locator('.compose-porthole').boundingBox();
    const nodes = await page.locator('.star').evaluateAll((items) => items.map((item) => {
      const b = item.getBoundingClientRect(); return { left: b.left, right: b.right };
    }));
    return nodes.every((node) => node.left >= frame!.x && node.right <= frame!.x + frame!.width);
  }).toBe(true);
  await page.screenshot({ path: testInfo.outputPath('compose-library-narrow.png'), fullPage: true });
});
