import type { Page } from '@playwright/test';

/**
 * Deterministic API fixtures. One pipeline ("gates", three stages a → b → c)
 * and one live run: a succeeded, b is running (so the a→b edge is hot and
 * carries the comet under full motion), c is queued.
 */
export const GATES_PIPELINE = {
  id: 'gates',
  name: 'gates',
  description: 'Three chained approval gates',
  stages: [
    { id: 'a', name: 'gate-a', type: 'approval', config: {}, position: { x: 120, y: 200 } },
    { id: 'b', name: 'gate-b', type: 'approval', config: {}, position: { x: 420, y: 140 } },
    { id: 'c', name: 'gate-c', type: 'approval', config: {}, position: { x: 720, y: 220 } },
  ],
  edges: [
    { id: 'ab', source: 'a', target: 'b' },
    { id: 'bc', source: 'b', target: 'c' },
  ],
  variables: {},
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
  version: 1,
};

export const GATES_RUN = {
  id: 'r1',
  pipelineId: 'gates',
  status: 'running',
  stageRuns: [
    { stageId: 'a', status: 'success', startedAt: '2026-09-05T00:00:00Z', finishedAt: '2026-09-05T00:00:03Z' },
    { stageId: 'b', status: 'running', startedAt: '2026-09-05T00:00:03Z', finishedAt: null },
    { stageId: 'c', status: 'pending', startedAt: null, finishedAt: null },
  ],
  environmentStatuses: [],
  variables: {},
  startedAt: '2026-09-05T00:00:00Z',
  finishedAt: null,
  startedByEmail: 'e2e@cooker.local',
};

const FREE_LICENSE = {
  status: 'none',
  plan: 'free',
  seats: 0,
  features: [],
  entitlements: { plan: 'free', seats: 0, features: {} },
  customer: '',
  issuedAt: '',
  expiresAt: null,
  installedAt: '',
  installedByEmail: '',
};

/**
 * Answer every /api/v1 request from fixtures. Playwright runs route handlers
 * most-recently-registered first, so the JSON 404 catch-all goes in FIRST and
 * the specific routes after it; anything unmocked surfaces as a 404 with the
 * URL in its body instead of hanging.
 */
export async function mockApi(page: Page): Promise<void> {
  await page.route('**/api/v1/**', (route) =>
    route.fulfill({ status: 404, json: { error: `unmocked route: ${route.request().method()} ${route.request().url()}` } }),
  );
  // WebSocket tickets: 403 makes the hooks stop quietly (401 would redirect to sign-in).
  await page.route('**/api/v1/ws-tickets', (route) => route.fulfill({ status: 403, json: { error: 'forbidden' } }));
  await page.route('**/api/v1/capabilities', (route) => route.fulfill({ json: { aiTriage: false, cloudInventory: false, feedback: false } }));
  await page.route('**/api/v1/environments**', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/v1/settings/registries', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/v1/settings/clusters', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/v1/tokens**', (route) => route.fulfill({ json: { tokens: [] } }));
  await page.route('**/api/v1/license', (route) => route.fulfill({ json: FREE_LICENSE }));
  await page.route('**/api/v1/pipelines?*', (route) => route.fulfill({ json: [GATES_PIPELINE] }));
  await page.route('**/api/v1/pipelines', (route) => route.fulfill({ json: [GATES_PIPELINE] }));
  await page.route('**/api/v1/pipelines/gates', (route) => route.fulfill({ json: GATES_PIPELINE }));
  await page.route('**/api/v1/pipelines/gates/runs**', (route) => route.fulfill({ json: [GATES_RUN] }));
  await page.route('**/api/v1/pipelines/gates/runs/r1', (route) => route.fulfill({ json: GATES_RUN }));
  await page.route('**/api/v1/pipelines/gates/runs/r1/stage-approvals', (route) => route.fulfill({ json: { runId: 'r1', gates: [] } }));
  await page.route('**/api/v1/pipelines/gates/runs/r1/env-status', (route) => route.fulfill({ json: { runId: 'r1', statuses: [] } }));
  await page.route('**/api/v1/pipelines/gates/runs/r1/logs/*', (route) => route.fulfill({ json: { logs: '' } }));
}
