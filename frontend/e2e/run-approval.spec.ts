import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { GATES_PIPELINE, GATES_RUN, mockApi } from './fixtures/api';
import type { Pipeline, PipelineRun, StageApproval } from '../src/types/pipeline';

// Owned UI contract fixtures only. No actual stage, promotion or paid triage runs.
const pipeline: Pipeline = { ...GATES_PIPELINE, version: 9, stages: [{ id: 'gate', name: 'Review deployment', type: 'approval', config: {}, position: { x: 0, y: 0 } }], edges: [] };
const run: PipelineRun = { ...GATES_RUN, pipelineVersion: 8, status: 'running', stageRuns: [{ stageId: 'gate', status: 'running', startedAt: '2026-10-02T20:00:00Z' }], environmentStatuses: [] };
const gate: StageApproval = { id: 'owned-gate', runId: 'r1', stageId: 'gate', status: 'awaiting', requiredApprovers: 2, votes: [] };
type Role = 'admin' | 'approver' | 'operator' | 'viewer';

async function setup(page: Page, role: Role, options: { missing?: boolean; failedRead?: boolean; decision?: 'complete' | 'failed' | 'conflict'; removed?: boolean; staleFirst?: boolean; promotionPartial?: boolean } = {}) {
  await mockApi(page); await page.addInitScript(() => localStorage.setItem('cooker.local.token', 'owned-approval-session'));
  await page.route('**/api/v1/auth/local/me', (route) => route.fulfill({ json: { id: role, name: role, email: `${role}@fixture.invalid`, roles: [role], role } }));
  let currentGate = structuredClone(gate); let readFailure = !!options.failedRead; let readCount = 0;
  const decisions: { kind: string; note: string; runId: string; stageId: string }[] = []; const promotions: { environmentId: string }[] = []; let promotionCount = 1; const otherWrites: string[] = []; const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.route('**/api/v1/pipelines/gates', (route) => route.fulfill({ json: options.removed ? { ...pipeline, stages: [{ ...pipeline.stages[0], id: 'replacement', name: 'Current replacement', type: 'custom' }] } : pipeline }));
  await page.route('**/api/v1/pipelines/gates/runs/r1', (route) => route.fulfill({ json: run }));
  await page.route('**/api/v1/pipelines/gates/runs/r2', (route) => route.fulfill({ json: { ...run, id: 'r2' } }));
  let releaseFirst: (() => void) | undefined;
  const firstHeld = new Promise<void>((resolve) => { releaseFirst = resolve; });
  await page.route('**/api/v1/pipelines/gates/runs/*/stage-approvals', async (route) => {
    const rid = new URL(route.request().url()).pathname.split('/')[6]; readCount++;
    if (options.staleFirst && rid === 'r1' && readCount === 1) await firstHeld;
    if (readFailure) return route.fulfill({ status: 503, json: { error: 'owned-gate-read-error-canary' } });
    return route.fulfill({ json: { runId: rid, gates: options.missing || rid === 'r2' ? [] : [currentGate] } });
  });
  await page.route('**/api/v1/pipelines/gates/runs/r1/stages/gate/*', (route) => {
    const kind = new URL(route.request().url()).pathname.split('/').at(-1)!; const body = route.request().postDataJSON() as { note: string }; decisions.push({ kind, note: body.note, runId: 'r1', stageId: 'gate' });
    if (options.decision === 'failed') return route.fulfill({ status: 500, json: { error: 'owned-gate-decision-error-canary' } });
    if (options.decision === 'conflict') { currentGate = { ...currentGate, status: 'rejected' }; return route.fulfill({ status: 409, json: { error: 'owned-gate-conflict-error-canary', status: 'rejected' } }); }
    currentGate = kind === 'reject' ? { ...currentGate, status: 'rejected' } : { ...currentGate, status: options.decision === 'complete' ? 'approved' : 'awaiting', votes: [{ approverEmail: `${role}@fixture.invalid`, note: body.note, createdAt: '2026-10-02T20:01:00Z' }] };
    if (options.decision === 'complete' && kind === 'approve') currentGate.votes = [...(currentGate.votes ?? []), { approverEmail: 'second@fixture.invalid', createdAt: '2026-10-02T20:00:00Z' }];
    return route.fulfill({ json: { runId: 'r1', stageId: 'gate', status: currentGate.status, approvalsHave: currentGate.votes?.length, approvalsNeed: currentGate.requiredApprovers, gate: currentGate } });
  });
  await page.route('**/api/v1/environments?*', (route) => route.fulfill({ json: [{ id: 'manual', name: 'Review environment', order: 1, promotion: { strategy: 'manual', requiredApprovers: options.promotionPartial ? 3 : 2 } }, { id: 'new', name: 'New destination', order: 2, promotion: { strategy: 'auto' } }] }));
  await page.route('**/api/v1/pipelines/gates/runs/r1/env-status', (route) => route.fulfill({ json: { runId: 'r1', statuses: [{ environmentId: 'manual', status: 'awaiting_approval', approvalsHave: promotionCount, approvalsNeed: options.promotionPartial ? 3 : 2 }] } }));
  await page.route('**/api/v1/pipelines/gates/runs/r1/approve', (route) => {
    promotions.push(route.request().postDataJSON() as { environmentId: string }); promotionCount++;
    return route.fulfill({ json: { status: options.promotionPartial ? 'awaiting_approval' : 'approved', approvalsHave: promotionCount, approvalsNeed: options.promotionPartial ? 3 : 2 } });
  });
  page.on('request', (request) => { const url = new URL(request.url()); if (url.pathname.startsWith('/api/v1/') && request.method() !== 'GET' && !url.pathname.includes('ws-tickets') && !url.pathname.endsWith('/runs/r1/approve') && !/\/stages\/gate\/(approve|reject)$/.test(url.pathname)) otherWrites.push(request.method() + ' ' + url.pathname); });
  return { decisions, promotions, otherWrites, pageErrors, failRead: (failed: boolean) => { readFailure = failed; }, releaseFirst: () => releaseFirst?.(), reads: () => readCount };
}

async function inspect(page: Page) {
  await page.goto('/pipelines/gates/runs/r1'); await page.getByRole('button', { name: 'Inspect recorded gate gate · awaiting', exact: true }).click(); return page.getByRole('complementary', { name: 'Stage Review deployment', exact: true });
}

for (const role of ['viewer', 'operator', 'admin', 'approver'] as const) test(`${role} stage gate controls match server approval authority`, async ({ page }) => {
  const fixture = await setup(page, role); const inspector = await inspect(page); const canApprove = role === 'admin' || role === 'approver';
  await expect(inspector).toContainText('0 of 2 approvals');
  await expect(inspector.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(canApprove ? 1 : 0); await expect(inspector.getByRole('button', { name: 'Reject', exact: true })).toHaveCount(canApprove ? 1 : 0); await expect(inspector.getByRole('textbox', { name: /^Approval note/ })).toHaveCount(canApprove ? 1 : 0);
  if (!canApprove) await expect(inspector).toContainText('administrator or dedicated approver');
  expect(fixture.decisions).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

for (const decision of ['partial', 'complete'] as const) test(`HTTP200 ${decision} approval distinguishes vote recording from gate completion`, async ({ page }) => {
  const fixture = await setup(page, 'approver', { decision: decision === 'complete' ? 'complete' : undefined }); const inspector = await inspect(page); await inspector.getByRole('textbox', { name: /^Approval note/ }).fill('Owned reviewed note'); await inspector.getByRole('button', { name: 'Approve', exact: true }).click();
  await expect(page.getByText(decision === 'partial' ? 'Approval recorded. This gate still needs more approvals.' : 'Approval gate approved.', { exact: true })).toBeVisible();
  expect(fixture.decisions).toEqual([{ kind: 'approve', note: 'Owned reviewed note', runId: 'r1', stageId: 'gate' }]);
  if (decision === 'partial') { await expect(inspector).toContainText('1 of 2 approvals'); await expect(inspector.getByRole('textbox', { name: /^Approval note/ })).toHaveValue(''); } else { await expect(inspector.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0); await expect(inspector).toContainText('approved'); }
  expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

for (const decision of ['failed', 'conflict'] as const) test(`${decision} decision retains the note and only shows fixed error summaries`, async ({ page }) => {
  const fixture = await setup(page, 'admin', { decision }); const inspector = await inspect(page); await inspector.getByRole('textbox', { name: /^Approval note/ }).fill('Retained owned note'); await inspector.getByRole('button', { name: 'Approve', exact: true }).click(); await expect(inspector.getByRole('alert')).toContainText('Your note is retained');
  await expect(inspector.getByRole('textbox', { name: decision === 'failed' ? /^Approval note/ : 'Retained approval note' })).toHaveValue('Retained owned note'); await expect(page.locator('body')).not.toContainText('owned-gate-decision-error-canary'); await expect(page.locator('body')).not.toContainText('owned-gate-conflict-error-canary'); expect(fixture.decisions).toHaveLength(1); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('an empty or failed gate read does not invent an actionable awaiting gate', async ({ page }) => {
  const fixture = await setup(page, 'admin', { failedRead: true }); await page.goto('/pipelines/gates/runs/r1'); const gates = page.getByRole('region', { name: 'Recorded approval gates', exact: true }); await expect(gates.getByRole('alert')).toContainText('could not be verified'); await expect(page.locator('body')).not.toContainText('owned-gate-read-error-canary'); await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0);
  fixture.failRead(false); await gates.getByRole('button', { name: 'Refresh approval gates', exact: true }).click(); await page.getByRole('button', { name: 'Inspect recorded gate gate · awaiting', exact: true }).click(); const inspector = page.getByRole('complementary', { name: 'Stage Review deployment', exact: true }); await expect(inspector.getByRole('button', { name: 'Approve', exact: true })).toBeEnabled(); fixture.failRead(true); await gates.getByRole('button', { name: 'Refresh approval gates', exact: true }).click(); await expect(gates.getByRole('alert')).toBeVisible(); await expect(inspector.getByRole('button', { name: 'Approve', exact: true })).toBeDisabled(); expect(fixture.decisions).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('an explicit empty snapshot does not turn a running stage into an approval record', async ({ page }) => {
  const fixture = await setup(page, 'admin', { missing: true }); await page.goto('/pipelines/gates/runs/r1'); await expect(page.getByText('No recorded approval gates are reported for this run.', { exact: true })).toBeVisible(); await page.locator('.react-flow__node[data-id="gate"]').click(); const inspector = page.getByRole('complementary', { name: 'Stage Review deployment', exact: true }); await expect(inspector).toContainText('No verified gate record is available'); await expect(inspector.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0); await expect(inspector.getByRole('button', { name: 'Reject', exact: true })).toHaveCount(0); expect(fixture.decisions).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('a gate removed from the current graph remains inspectable with its accepted version', async ({ page }) => {
  const fixture = await setup(page, 'approver', { removed: true }); await page.goto('/pipelines/gates/runs/r1'); await page.getByRole('button', { name: 'Inspect recorded gate gate · awaiting', exact: true }).click(); const inspector = page.getByRole('complementary', { name: 'Stage Recorded approval gate', exact: true }); await expect(inspector).toContainText('accepted pipeline version 8 · current definition version 9'); await expect(inspector).toContainText('original stage name and configuration are not reconstructed'); await expect(inspector.getByRole('button', { name: 'Approve', exact: true })).toBeEnabled(); expect(fixture.decisions).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('a delayed prior-run gate response cannot authorize the next run', async ({ page }) => {
  const fixture = await setup(page, 'admin', { staleFirst: true }); await page.goto('/pipelines/gates/runs/r1'); await expect.poll(fixture.reads).toBe(1); /* Preserve the mounted route/hook while simulating client navigation. */ await page.evaluate(() => { history.pushState(history.state, '', '/pipelines/gates/runs/r2'); dispatchEvent(new PopStateEvent('popstate', { state: history.state })); }); await expect(page.getByText('No recorded approval gates are reported for this run.', { exact: true })).toBeVisible(); const oldResponse = page.waitForResponse((response) => response.url().endsWith('/runs/r1/stage-approvals')); fixture.releaseFirst(); await oldResponse; await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))); await expect(page.getByRole('button', { name: 'Inspect recorded gate gate · awaiting', exact: true })).toHaveCount(0); await expect(page.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0); expect(fixture.decisions).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

for (const role of ['operator', 'approver'] as const) test(`${role} promotion controls separate invoking from approving`, async ({ page }) => {
  const fixture = await setup(page, role); await page.goto('/pipelines/gates/runs/r1'); await page.getByRole('button', { name: 'Promote', exact: true }).click(); const panel = page.getByRole('complementary', { name: 'Environment promotion', exact: true }); await expect(panel.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(role === 'approver' ? 1 : 0); await expect(panel.getByRole('button', { name: 'Promote here', exact: true })).toHaveCount(role === 'operator' ? 1 : 0); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('a recorded promotion vote below quorum does not claim completed approval or deployment', async ({ page }) => {
  const fixture = await setup(page, 'approver', { promotionPartial: true }); await page.goto('/pipelines/gates/runs/r1'); await page.getByRole('button', { name: 'Promote', exact: true }).click(); const panel = page.getByRole('complementary', { name: 'Environment promotion', exact: true }); await panel.getByRole('button', { name: 'Approve', exact: true }).click(); await panel.getByRole('button', { name: 'Approve Review environment?', exact: true }).click(); await expect(page.getByText('Promotion approval recorded. More approvals are still required.', { exact: true })).toBeVisible(); await expect(panel).toContainText('2/3 approvals'); expect(fixture.promotions).toEqual([{ environmentId: 'manual' }]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('task guides explain native rules, explicit registry grants, quorum and Vercel URL access', async ({ page }) => {
  const fixture = await setup(page, 'viewer'); await page.goto('/docs?topic=native-git'); const article = page.locator('article.workspace-guide-article'); await expect(article.getByRole('heading', { name: 'Connect Git to a native pipeline', exact: true })).toBeVisible(); await expect(article).toContainText('accepted source receipt'); await page.getByRole('button', { name: /^Choose registry access/ }).click(); await expect(article).toContainText('Saving a credential does not grant every application access'); await page.getByRole('button', { name: /^Follow progress and logs/ }).click(); await expect(article).toContainText('awaiting more distinct approvers'); await page.getByRole('button', { name: /^Connect Vercel/ }).click(); await expect(article).toContainText('deployment URL may require authentication'); expect(fixture.decisions).toEqual([]); expect(fixture.promotions).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('narrow keyboard gate inspection exposes an accessible labelled note', async ({ page }, testInfo) => {
  const fixture = await setup(page, 'approver'); await page.setViewportSize({ width: 390, height: 844 }); const inspector = await inspect(page); await inspector.getByRole('textbox', { name: /^Approval note/ }).focus(); await page.keyboard.type('Keyboard review'); await expect(inspector.getByRole('textbox', { name: /^Approval note/ })).toHaveValue('Keyboard review'); expect((await new AxeBuilder({ page }).include('.inspector').analyze()).violations.filter((item) => ['critical', 'serious'].includes(item.impact ?? ''))).toEqual([]); await page.screenshot({ path: testInfo.outputPath('approval-narrow.png') }); expect(fixture.decisions).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});


// Legacy Settings registry subsection: bounded checks only; other sections are outside this slice.
test('Settings registry controls are administrator-only across all four roles', async ({ page }) => {
  for (const role of ['viewer', 'operator', 'approver', 'admin'] as const) {
    const fixture = await setup(page, role);
    await page.route('**/api/v1/settings/registries', (route) => route.fulfill({ json: [{ id: 'owned-registry', name: 'Owned registry', url: 'registry.fixture.invalid', version: 1, hasPassword: true }] }));
    await page.goto('/settings'); const section = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Image registries', exact: true }) });
    await expect(section).toContainText('Owned registry');
    await expect(section.getByRole('button', { name: '＋ Add registry', exact: true })).toHaveCount(role === 'admin' ? 1 : 0);
    await expect(section.getByRole('button', { name: 'Remove', exact: true })).toHaveCount(role === 'admin' ? 1 : 0);
    await expect(section.getByRole('link', { name: 'Registry', exact: true })).toHaveAttribute('href', '/registry');
    await expect(section).toContainText('adding one alone does not enable credential use');
    if (role !== 'admin') await expect(section).toContainText('An administrator manages registry connections and grants');
    expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
  }
});

test('a failed Settings registry read stays unavailable rather than claiming no connections', async ({ page }) => {
  const fixture = await setup(page, 'admin'); await page.route('**/api/v1/settings/registries', (route) => route.fulfill({ status: 503, json: { error: 'owned-settings-read-canary' } }));
  await page.goto('/settings'); const section = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Image registries', exact: true }) });
  await expect(section).toContainText('Registry connections could not be loaded. Refresh before changing access.'); await expect(section).not.toContainText('No registry connections configured.'); await expect(page.locator('body')).not.toContainText('owned-settings-read-canary'); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('a failed Settings registry save keeps the owned draft and shows a fixed error', async ({ page }) => {
  const fixture = await setup(page, 'admin'); let registryWrites = 0;
  await page.route('**/api/v1/settings/registries', (route) => {
    if (route.request().method() === 'GET') return route.fulfill({ json: [] });
    registryWrites++; return route.fulfill({ status: 500, json: { error: 'owned-settings-save-canary' } });
  });
  await page.goto('/settings'); const section = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Image registries', exact: true }) }); await section.getByRole('button', { name: '＋ Add registry', exact: true }).click();
  await section.getByRole('textbox', { name: 'Name', exact: true }).fill('Owned draft'); await section.getByRole('textbox', { name: 'URL', exact: true }).fill('registry.fixture.invalid'); await section.getByLabel('Password / token', { exact: true }).fill('owned-synthetic-password'); await section.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(page.getByText('Registry connection change could not be saved. Refresh its current status before retrying.', { exact: true }).first()).toBeVisible(); await expect(section.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue('Owned draft'); await expect(section.getByRole('textbox', { name: 'URL', exact: true })).toHaveValue('registry.fixture.invalid'); await expect(section.getByLabel('Password / token', { exact: true })).toHaveValue('owned-synthetic-password'); await expect(page.locator('body')).not.toContainText('owned-settings-save-canary'); await expect(page.locator('body')).not.toContainText('owned-synthetic-password'); expect(registryWrites).toBe(1); expect(fixture.pageErrors).toEqual([]);
});

for (const missing of ['environments', 'statuses'] as const) test(`malformed ${missing} promotion data stays unavailable without mutation controls`, async ({ page }) => {
  const fixture = await setup(page, 'admin');
  if (missing === 'environments') await page.route('**/api/v1/environments?*', (route) => route.fulfill({ json: null }));
  else await page.route('**/api/v1/pipelines/gates/runs/r1/env-status', (route) => route.fulfill({ json: { runId: 'r1', statuses: null } }));
  await page.goto('/pipelines/gates/runs/r1'); await page.getByRole('button', { name: 'Promote', exact: true }).click(); const panel = page.getByRole('complementary', { name: 'Environment promotion', exact: true });
  await expect(panel).toContainText('could not be loaded'); await expect(panel.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0); await expect(panel.getByRole('button', { name: 'Promote here', exact: true })).toHaveCount(0); await expect(panel).not.toContainText('No environments configured yet.'); expect(fixture.promotions).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('an older promotion response cannot replace a newer snapshot or restore approval controls', async ({ page }) => {
  const fixture = await setup(page, 'approver'); let requests = 0; let releaseOlder: (() => void) | undefined; const held = new Promise<void>((resolve) => { releaseOlder = resolve; });
  await page.clock.install();
  await page.route('**/api/v1/pipelines/gates/runs/r1/env-status', async (route) => {
    const observation = ++requests; if (observation === 2) await held;
    return route.fulfill({ json: { runId: 'r1', statuses: [{ environmentId: 'manual', status: observation < 3 ? 'awaiting_approval' : 'approved', approvalsHave: observation < 3 ? 1 : 2, approvalsNeed: 2 }] } });
  });
  await page.goto('/pipelines/gates/runs/r1'); await page.getByRole('button', { name: 'Promote', exact: true }).click(); const panel = page.getByRole('complementary', { name: 'Environment promotion', exact: true }); await expect(panel.getByRole('button', { name: 'Approve', exact: true })).toBeVisible(); await panel.getByRole('button', { name: 'Refresh promotion status', exact: true }).click(); await expect.poll(() => requests).toBe(2);
  await page.clock.fastForward(5001); await expect.poll(() => requests).toBeGreaterThanOrEqual(3); await expect(panel.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0); const oldResponse = page.waitForResponse((response) => response.url().endsWith('/runs/r1/env-status')); releaseOlder?.(); await oldResponse; await page.evaluate(() => new Promise<void>((resolve) => setTimeout(resolve, 0))); await expect(panel.getByRole('button', { name: 'Approve', exact: true })).toHaveCount(0); expect(fixture.promotions).toEqual([]); expect(fixture.otherWrites).toEqual([]); expect(fixture.pageErrors).toEqual([]);
});

test('a foreign gate response cannot confirm this run decision or clear its note', async ({ page }) => {
  const fixture = await setup(page, 'approver'); await page.route('**/api/v1/pipelines/gates/runs/r1/stages/gate/approve', (route) => route.fulfill({ json: { runId: 'r2', stageId: 'gate', status: 'awaiting', approvalsHave: 1, approvalsNeed: 2, gate: { ...gate, runId: 'r2', votes: [{ approverEmail: 'approver@fixture.invalid', createdAt: '2026-10-02T20:01:00Z' }] } } }));
  const inspector = await inspect(page); await inspector.getByRole('textbox', { name: /^Approval note/ }).fill('Unconfirmed owned note'); await inspector.getByRole('button', { name: 'Approve', exact: true }).click(); await expect(inspector.getByRole('alert')).toContainText('Your note is retained'); await expect(inspector.getByRole('textbox', { name: /^Approval note/ })).toHaveValue('Unconfirmed owned note'); await expect(page.getByText('Approval recorded. This gate still needs more approvals.', { exact: true })).toHaveCount(0); expect(fixture.pageErrors).toEqual([]);
});
