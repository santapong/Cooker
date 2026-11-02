import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { GATES_PIPELINE, GATES_RUN, mockApi } from './fixtures/api';
import type { Pipeline } from '../src/types/pipeline';

// Frontend contract fixtures only. No source checkout, worker or deployment runs.
const plan: Pipeline = {
  ...GATES_PIPELINE, name: 'Reviewed release',
  stages: [
    { id: 'a', name: 'Build image', type: 'build', config: { dockerfile: 'Dockerfile' }, position: { x: 20, y: 20 } },
    { id: 'b', name: 'Check policy', type: 'test', config: { image: 'alpine:3' }, position: { x: 20, y: 240 } },
    { id: 'c', name: 'Release app', type: 'deploy', config: {}, position: { x: 380, y: 130 } },
    { id: 'd', name: 'Notify team', type: 'custom', config: {}, position: { x: 740, y: 130 } },
  ],
  edges: [{ id: 'ac', source: 'a', target: 'c' }, { id: 'bc', source: 'b', target: 'c' }, { id: 'cd', source: 'c', target: 'd', condition: 'always' }],
};

async function setup(page: Page, role: 'admin' | 'operator' | 'viewer' = 'admin', initial = plan) {
  await mockApi(page);
  await page.addInitScript(() => localStorage.setItem('cooker.local.token', 'fixture-dag-session'));
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: `fixture-${role}`, email: `${role}@cooker.test`, name: role, role, roles: [role] } }));
  let current = structuredClone(initial);
  const saved: Pipeline[] = []; const runs: string[] = [];
  await page.route('**/api/v1/pipelines/gates', (route) => {
    if (route.request().method() === 'PUT') {
      const input = route.request().postDataJSON() as Pipeline;
      saved.push(structuredClone(input)); current = { ...input, version: (current.version ?? 1) + 1 };
    }
    return route.fulfill({ json: current });
  });
  await page.route('**/api/v1/pipelines/gates/validate', (route) => route.fulfill({ json: { valid: true, errors: [] } }));
  await page.route('**/api/v1/pipelines/gates/run', (route) => { runs.push(route.request().method()); return route.fulfill({ json: GATES_RUN }); });
  await page.goto('/pipelines/gates/edit');
  await expect(page.locator('.pipeline-stage-card')).toHaveCount(initial.stages.length);
  await expect(page.locator('.pipeline-dependency-number')).toHaveCount(initial.edges.length);
  return { saved, runs };
}

const dependency = (page: Page, number: number) => page.locator('.pipeline-dependency-number').filter({ hasText: new RegExp(`^${number}$`) });

test('selecting a numbered arrow explains the join without changing its condition; explicit edits keep edge IDs', async ({ page }, testInfo) => {
  const fixture = await setup(page);
  await dependency(page, 1).click();
  const inspector = page.getByRole('complementary', { name: 'Dependency explanation' });
  await expect(inspector).toContainText('Release app waits until Build image succeeds.');
  await expect(inspector).toContainText('their conditions must also be met');
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  expect(fixture.saved).toEqual([]);
  await inspector.getByText('Edit dependency', { exact: true }).click();
  await inspector.getByRole('combobox', { name: 'Condition', exact: true }).selectOption('failure');
  await expect(inspector).toContainText('Release app waits until Build image fails.');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  expect(fixture.saved[0].edges.map((edge) => edge.id)).toEqual(['ac', 'bc', 'cd']);
  expect(fixture.saved[0].edges[0].condition).toBe('failure');
  expect(fixture.saved[0].stages.map((stage) => stage.id)).toEqual(['a', 'b', 'c', 'd']);
  expect(await page.locator('.pipeline-dependency-line').first().getAttribute('marker-end')).toMatch(/^url\(#dependency-/);
  expect((await new AxeBuilder({ page }).include('main').analyze()).violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([]);
  await page.screenshot({ path: testInfo.outputPath('dependency-explanation.png'), fullPage: true });
});

test('step list exposes every prerequisite and preserves rename, save, validate and run', async ({ page }) => {
  const fixture = await setup(page);
  await page.getByRole('button', { name: 'Step list', exact: true }).click();
  await page.locator('.pipeline-list-stage').filter({ hasText: 'Release app' }).click();
  const inspector = page.getByRole('complementary', { name: 'Stage Release app', exact: true });
  await expect(inspector.getByRole('heading', { name: 'Waits for all 2 requirements' })).toBeVisible();
  await expect(inspector.locator('.pipeline-prerequisites li')).toHaveCount(2);
  await inspector.getByText('Edit step and connections', { exact: true }).click();
  await inspector.getByLabel('Stage name', { exact: true }).fill('Release reviewed app');
  await page.getByRole('button', { name: 'Validate', exact: true }).click();
  await expect(page.getByText('Save your changes before validating the server definition.', { exact: true })).toBeVisible();
  expect(fixture.saved).toHaveLength(0);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  expect(fixture.saved[0].stages.find((stage) => stage.id === 'c')?.name).toBe('Release reviewed app');
  await page.getByRole('button', { name: 'Validate', exact: true }).click();
  await expect(page.getByText('Pipeline DAG is valid.', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Run pipeline', exact: true }).click();
  await expect(page).toHaveURL(/\/pipelines\/gates\/runs\/r1$/);
  expect(fixture.runs).toEqual(['POST']);
});

test('the connection editor rejects cycles, adds a valid prerequisite, then removes it explicitly', async ({ page }) => {
  const fixture = await setup(page, 'operator');
  await page.getByRole('button', { name: 'Step list', exact: true }).click();
  await page.locator('.pipeline-list-stage').filter({ hasText: 'Check policy' }).click();
  const inspector = page.getByRole('complementary', { name: 'Stage Check policy', exact: true });
  await inspector.getByText('Edit step and connections', { exact: true }).click();
  await inspector.getByRole('combobox', { name: 'Add a prerequisite', exact: true }).selectOption('d');
  await inspector.getByRole('button', { name: 'Add dependency', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'would create a cycle' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await inspector.getByRole('combobox', { name: 'Add a prerequisite', exact: true }).selectOption('a');
  await inspector.getByRole('button', { name: 'Add dependency', exact: true }).click();
  await inspector.locator('.pipeline-prerequisites button').click();
  const explanation = page.getByRole('complementary', { name: 'Dependency explanation' });
  await explanation.getByText('Edit dependency', { exact: true }).click();
  await explanation.getByRole('button', { name: 'Remove dependency', exact: true }).click();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  expect(fixture.saved[0].edges.map((edge) => edge.id)).toEqual(plan.edges.map((edge) => edge.id));
});

test('viewers can inspect dependencies and configuration without editing, removing, saving or running', async ({ page }) => {
  const fixture = await setup(page, 'viewer');
  await expect(page.getByRole('toolbar', { name: 'Add stage' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Run pipeline', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toHaveCount(0);
  await dependency(page, 3).click();
  const inspector = page.getByRole('complementary', { name: 'Dependency explanation' });
  await expect(inspector).toContainText('finishes or is skipped');
  await inspector.getByText('Technical details', { exact: true }).click();
  await expect(inspector.getByRole('combobox', { name: 'Condition', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Remove dependency', exact: true })).toHaveCount(0);
  expect(fixture.saved).toEqual([]); expect(fixture.runs).toEqual([]);
});

test('adding and removing a real stage leaves the other stage identities intact', async ({ page }) => {
  const fixture = await setup(page);
  await page.getByRole('toolbar', { name: 'Add stage' }).getByRole('button', { name: 'Build', exact: true }).click();
  await expect(page.locator('.pipeline-stage-card')).toHaveCount(5);
  const inspector = page.getByRole('complementary', { name: 'Stage Build Stage', exact: true });
  await inspector.getByText('Edit step and connections', { exact: true }).click();
  await inspector.getByRole('button', { name: 'Remove stage', exact: true }).click();
  await expect(page.locator('.pipeline-stage-card')).toHaveCount(4);
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  expect(fixture.saved[0].stages.map((stage) => stage.id)).toEqual(plan.stages.map((stage) => stage.id));
});

test('a rejected save remains unsaved and prevents Run from dispatching a different definition', async ({ page }) => {
  const fixture = await setup(page);
  await page.route('**/api/v1/pipelines/gates', (route) => route.request().method() === 'PUT' ? route.fulfill({ status: 409, json: { error: 'Pipeline changed on the server. Reload before saving.' } }) : route.fallback());
  await page.getByRole('button', { name: 'Step list', exact: true }).click();
  await page.locator('.pipeline-list-stage').filter({ hasText: 'Build image' }).click();
  const inspector = page.getByRole('complementary', { name: 'Stage Build image', exact: true });
  await inspector.getByText('Edit step and connections', { exact: true }).click();
  await inspector.getByLabel('Stage name', { exact: true }).fill('My changed build');
  await page.getByRole('button', { name: 'Run pipeline', exact: true }).click();
  await expect(page.getByText('Pipeline changed on the server. Reload before saving.', { exact: true })).toBeVisible();
  await expect(page.getByText('Unsaved changes', { exact: false })).toBeVisible();
  await expect(page).toHaveURL(/\/pipelines\/gates\/edit$/);
  expect(fixture.runs).toEqual([]);
});

test('fit includes dense graph labels on desktop and mobile; Find step returns to readable zoom', async ({ page }, testInfo) => {
  const stages = Array.from({ length: 22 }, (_, index) => ({ id: `s${index}`, name: `Real fixture step ${index+1}`, type: 'build' as const, config: {}, position: { x: Math.floor(index/4)*350, y: (index%4)*180 } }));
  const edges = stages.slice(4).flatMap((stage, index) => [{ id: `f${index}`, source: stages[index].id, target: stage.id }, ...(index < 11 ? [{ id: `j${index}`, source: stages[(index+1)%4].id, target: stage.id }] : [])]);
  await setup(page, 'admin', { ...plan, stages, edges });
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.getByRole('button', { name: 'Fit map', exact: true }).click();
    await expect.poll(async () => page.locator('.pipeline-map').evaluate((map) => {
      const box = map.getBoundingClientRect();
      return [...map.querySelectorAll('.pipeline-stage-card, .pipeline-dependency-number')].every((element) => { const item = element.getBoundingClientRect(); return item.left >= box.left-1 && item.right <= box.right+1 && item.top >= box.top-1 && item.bottom <= box.bottom+1; });
    }), { timeout: 10000 }).toBe(true);
    await expect(page.locator('.pipeline-dependency-number').first()).toHaveCSS('width', '44px');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath(`dense-dag-${width}.png`), fullPage: true });
  }
  await page.getByRole('button', { name: 'Step list', exact: true }).click();
  await page.getByLabel('Find a step', { exact: false }).selectOption('s20');
  await expect(page.locator('.pipeline-toolbar [role=group]').filter({ hasText: '100%' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Real fixture step 21', exact: true })).toBeVisible();
});
