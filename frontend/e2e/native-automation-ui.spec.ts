import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { GATES_PIPELINE, mockApi } from './fixtures/api';
import type { Pipeline } from '../src/types/pipeline';
import type { NativeTriggerPreview, NativeTriggerRule } from '../src/api/nativeAutomation';

// Owned API fixtures only: these UI contracts do not execute a webhook,
// inspect an actual repository, admit a deployment, or contact a provider.
const pipeline: Pipeline = { ...GATES_PIPELINE, version: 8, source: { connectionId: 'owned-source', repository: 'owned/app', fullRef: 'refs/heads/main' }, stages: [{ id: 'a', name: 'Owned build', type: 'build', position: { x: 20, y: 20 }, config: { dockerfile: 'Dockerfile', registryConnectionIds: [] } }], edges: [] };
const envelopeHash = 'b'.repeat(64); const planHash = 'a'.repeat(64); const commit = 'c'.repeat(40);
type Variant = 'valid' | 'contradictory' | 'missing-plan' | 'missing-envelope-hash' | 'missing-envelope' | 'failed';

async function setup(page: Page, role: 'admin' | 'operator' | 'viewer' = 'admin', variant: Variant = 'valid') {
  await mockApi(page);
  await page.addInitScript(() => localStorage.setItem('cooker.local.token', 'owned-native-rule-session'));
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: role, name: role, email: `${role}@fixture.invalid`, role, roles: [role] } }));
  let current = structuredClone(pipeline); let rules: NativeTriggerRule[] = []; let historyFail = false;
  const previews: { rule: NativeTriggerRule; ref: string }[] = [];
  const saves: { rule: NativeTriggerRule; ref: string; expectedPlanHash: string; expectedEnvelopeHash: string }[] = [];
  const pipelineSaves: Pipeline[] = []; const historyQueries: URL[] = []; const adminReads: URL[] = []; const otherWrites: string[] = []; const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.route('**/api/v1/settings/registries', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/v1/apps/source-connections', (route) => route.fulfill({ json: [{ id: 'owned-source', name: 'Owned source', provider: 'github', enabled: true, version: 2, repositories: ['owned/app'] }] }));
  await page.route('**/api/v1/pipelines/gates', (route) => {
    if (route.request().method() === 'PUT') { const body = route.request().postDataJSON() as Pipeline; pipelineSaves.push(structuredClone(body)); current = { ...body, version: (current.version ?? 1) + 1 }; }
    return route.fulfill({ json: current });
  });
  await page.route('**/api/v1/admin/trigger-rules/preview', (route) => {
    const body = route.request().postDataJSON() as { rule: NativeTriggerRule; ref: string }; previews.push(body);
    if (variant === 'failed') return route.fulfill({ status: 503, json: { error: 'owned-native-preview-error-canary' } });
    const result: NativeTriggerPreview = { rule: { ...body.rule, id: 'owned-rule', version: 1, pipelineVersion: current.version, planHash, approvedEnvelopeHash: envelopeHash }, envelope: { hash: envelopeHash, commit, files: ['Dockerfile', '.dockerignore'] } };
    if (variant === 'contradictory') result.rule.appId = 'wrong-app' as '';
    if (variant === 'missing-plan') delete result.rule.planHash;
    if (variant === 'missing-envelope-hash') result.envelope.hash = '';
    return route.fulfill({ json: variant === 'missing-envelope' ? { rule: result.rule } : result });
  });
  await page.route('**/api/v1/admin/trigger-rules', (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON() as typeof saves[number]; saves.push(body); rules = [{ ...body.rule, id: 'owned-rule', reviewedAt: '2026-10-02T20:00:00Z' }];
      return route.fulfill({ json: rules[0] });
    }
    adminReads.push(new URL(route.request().url())); return route.fulfill({ json: rules });
  });
  await page.route('**/api/v1/admin/trigger-deliveries**', (route) => {
    const url = new URL(route.request().url()); historyQueries.push(url); adminReads.push(url);
    if (historyFail) return route.fulfill({ status: 503, json: { error: 'owned-native-history-error-canary' } });
    const row = { deliveryId: 'owned-delivery', providerDeliveryId: 'owned-provider-delivery', provider: 'github', connectionId: 'owned-source', repository: 'owned/app', fullRef: saves[0]?.ref ?? 'refs/heads/main', commitSha: commit, deleted: false, ruleId: 'owned-rule', ruleVersion: 1, targetKind: 'pipeline', targetId: 'gates', targetSource: 'intent', metadataCoverage: 'retained_decision', intentId: 'owned-intent', intentStatus: 'queued', jobId: 'owned-job', jobStatus: 'succeeded', pipelineId: 'gates', runId: 'r1', runStatus: 'success', status: 'accepted', reasonCode: 'fixture', reason: 'Owned fixture: accepted records do not prove deployment success.', createdAt: '2026-10-02T20:00:00Z', updatedAt: '2026-10-02T20:01:00Z', archived: false, decisionIndex: 1 };
    return route.fulfill({ json: { items: saves.length ? [row] : [], total: saves.length ? 1 : 0, limit: 25, offset: 0, coverage: 'owned-fixture', limitations: ['This fixture does not execute a delivery or deployment.'] } });
  });
  page.on('request', (request) => {
    if (!request.url().includes('/api/v1/') || request.method() === 'GET' || request.url().includes('ws-tickets')) return;
    const path = new URL(request.url()).pathname;
    if (!['/api/v1/admin/trigger-rules', '/api/v1/admin/trigger-rules/preview', '/api/v1/pipelines/gates'].includes(path)) otherWrites.push(request.method() + ' ' + path);
  });
  return { previews, saves, pipelineSaves, historyQueries, adminReads, otherWrites, pageErrors, failHistory: (fail: boolean) => { historyFail = fail; } };
}

async function openRules(page: Page) {
  await page.goto('/pipelines/gates/edit');
  const panel = page.locator('details.pipeline-source-binding').filter({ has: page.locator('summary').filter({ hasText: /^Git deployment rules/ }) });
  await panel.locator('summary').first().click(); return panel;
}

for (const kind of ['branch', 'tag'] as const) test(`admin previews and reviews an exact ${kind} and inspects linked native history`, async ({ page }) => {
  const fixture = await setup(page); const panel = await openRules(page); const name = kind === 'branch' ? 'release/next' : 'v1.2.3'; const fullRef = `refs/${kind === 'branch' ? 'heads' : 'tags'}/${name}`;
  await panel.getByRole('combobox', { name: 'Watch Git refs', exact: true }).selectOption(kind);
  await panel.getByRole('textbox', { name: /^Included names/ }).fill(name);
  await panel.getByRole('textbox', { name: /^Concrete ref to inspect/ }).fill(name);
  await panel.getByRole('button', { name: 'Preview Git rule', exact: true }).click();
  await expect(panel.getByText(/Reviewed commit/)).toContainText(commit);
  await expect(panel.getByRole('button', { name: 'Approve configuration and save Git rule', exact: true })).toBeEnabled();
  expect(fixture.previews[0].ref).toBe(fullRef); expect(fixture.previews[0].rule).toMatchObject({ appId: '', targetKind: 'pipeline', targetId: 'gates', source: { connectionId: 'owned-source' }, refKind: kind, match: 'exact', include: [name], approval: 'review' });
  await panel.getByRole('button', { name: 'Approve configuration and save Git rule', exact: true }).click();
  await expect(panel.locator('article').filter({ hasText: 'Review each commit' })).toHaveCount(1);
  expect(fixture.saves).toHaveLength(1); expect(fixture.saves[0]).toMatchObject({ ref: fullRef, expectedPlanHash: planHash, expectedEnvelopeHash: envelopeHash, rule: { pipelineVersion: 8, targetKind: 'pipeline', targetId: 'gates', appId: '' } }); expect(fixture.saves[0]).not.toHaveProperty('envelope.graph');
  const history = panel.getByRole('region', { name: 'Git delivery history', exact: true }); await history.getByRole('button', { name: 'Refresh decisions', exact: true }).click();
  await expect(history.getByRole('article')).toHaveCount(1); expect(fixture.historyQueries.at(-1)?.searchParams.get('targetKind')).toBe('pipeline'); expect(fixture.historyQueries.at(-1)?.searchParams.get('targetId')).toBe('gates'); expect(fixture.historyQueries.at(-1)?.searchParams.has('appId')).toBe(false);
  await history.getByText('Inspect delivery, intent and job', { exact: true }).click();
  await expect(history.getByRole('link', { name: 'Open linked run', exact: true })).toHaveAttribute('href', '/pipelines/gates/runs/r1'); await history.getByRole('link', { name: 'Open linked run', exact: true }).click(); await expect(page).toHaveURL(/\/pipelines\/gates\/runs\/r1$/);
  expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('changed rule inputs and saved pipeline versions invalidate the prior preview', async ({ page }) => {
  const fixture = await setup(page); const panel = await openRules(page);
  await panel.getByRole('button', { name: 'Preview Git rule', exact: true }).click(); await expect(panel.getByRole('button', { name: 'Approve configuration and save Git rule', exact: true })).toBeEnabled();
  await panel.getByRole('textbox', { name: /^Concrete ref to inspect/ }).fill('release/next'); await expect(panel.getByRole('button', { name: 'Approve configuration and save Git rule', exact: true })).toHaveCount(0);
  await panel.getByRole('button', { name: 'Preview Git rule', exact: true }).click(); await expect(panel.getByRole('button', { name: 'Approve configuration and save Git rule', exact: true })).toBeEnabled();
  await page.locator('details.pipeline-source-binding').filter({ has: page.locator('summary').filter({ hasText: /^Source ·/ }) }).getByRole('textbox', { name: /^Full branch or tag ref/ }).fill('refs/tags/v2');
  await expect(panel.getByRole('status').filter({ hasText: 'Save your pipeline changes' })).toBeVisible(); await expect(panel.getByRole('button', { name: 'Preview Git rule', exact: true })).toBeDisabled(); await expect(panel.getByRole('button', { name: 'Approve configuration and save Git rule', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Save', exact: true }).click(); await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await panel.getByRole('button', { name: 'Preview Git rule', exact: true }).click(); await expect(panel.getByText(/Reviewed commit/)).toContainText('pipeline version 9');
  expect(fixture.pipelineSaves[0].version).toBe(8); expect(fixture.saves).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

for (const variant of ['contradictory', 'missing-plan', 'missing-envelope-hash'] as const) test(`${variant} preview cannot approve a native rule`, async ({ page }) => {
  const fixture = await setup(page, 'admin', variant); const panel = await openRules(page); await panel.getByRole('button', { name: 'Preview Git rule', exact: true }).click();
  await expect(panel.getByRole('alert')).toContainText('could not be verified'); await expect(panel.getByRole('button', { name: 'Approve configuration and save Git rule', exact: true })).toHaveCount(0);
  expect(fixture.saves).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

for (const variant of ['failed', 'missing-envelope'] as const) test(`${variant} preview stays safely unapproved without exposing error details`, async ({ page }) => {
  const fixture = await setup(page, 'admin', variant); const panel = await openRules(page); await panel.getByRole('button', { name: 'Preview Git rule', exact: true }).click();
  await expect(panel.getByRole('alert')).toContainText('could not be verified'); await expect(page.locator('body')).not.toContainText('owned-native-preview-error-canary'); await expect(panel.getByRole('button', { name: 'Approve configuration and save Git rule', exact: true })).toHaveCount(0);
  expect(fixture.saves).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('native history failures show a fixed explanation and refresh the exact pipeline scope', async ({ page }) => {
  const fixture = await setup(page); fixture.failHistory(true); const panel = await openRules(page); const history = panel.getByRole('region', { name: 'Git delivery history', exact: true });
  await expect(history.getByRole('alert')).toBeVisible(); await expect(history).not.toContainText('owned-native-history-error-canary'); fixture.failHistory(false); await history.getByRole('button', { name: 'Refresh decisions', exact: true }).click(); await expect(history.getByRole('status')).toHaveText('No matching decisions');
  expect(fixture.historyQueries.at(-1)?.searchParams.get('targetId')).toBe('gates'); expect(fixture.saves).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

for (const role of ['operator', 'viewer'] as const) test(`${role} sees the administrator boundary without native rule/history reads or writes`, async ({ page }) => {
  const fixture = await setup(page, role); await page.goto('/pipelines/gates/edit'); await expect(page.getByText('An administrator can configure exact branch or release-tag rules and inspect their delivery decisions.', { exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Preview Git rule', exact: true })).toHaveCount(0); await expect(page.getByRole('region', { name: 'Git delivery history', exact: true })).toHaveCount(0);
  expect(fixture.adminReads).toEqual([]); expect(fixture.previews).toEqual([]); expect(fixture.saves).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

for (const theme of ['dark', 'light']) test(`${theme} native Git rules support narrow keyboard review and accessibility`, async ({ page }, testInfo) => {
  const fixture = await setup(page); await page.addInitScript((themeMode) => localStorage.setItem('cooker-ui', JSON.stringify({ state: { themeMode, calmMode: true }, version: 0 })), theme); await page.setViewportSize({ width: 390, height: 844 }); const panel = await openRules(page);
  const watch = panel.getByRole('combobox', { name: 'Watch Git refs', exact: true }); await watch.focus(); await page.keyboard.press('ArrowDown'); await page.keyboard.press('Enter'); await expect(watch).toHaveValue('tag');
  await panel.getByRole('textbox', { name: /^Included names/ }).fill('v1'); await panel.getByRole('textbox', { name: /^Concrete ref to inspect/ }).fill('v1'); await panel.getByRole('button', { name: 'Preview Git rule', exact: true }).click(); await expect(panel.getByRole('button', { name: 'Approve configuration and save Git rule', exact: true })).toBeEnabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true); expect((await new AxeBuilder({ page }).include('details.pipeline-source-binding').analyze()).violations.filter((item) => ['critical', 'serious'].includes(item.impact ?? ''))).toEqual([]);
  await page.screenshot({ path: testInfo.outputPath(`native-rules-${theme}.png`), fullPage: true }); expect(fixture.saves).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});
