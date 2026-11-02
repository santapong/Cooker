import { test, expect, type Page } from '@playwright/test';
import { mockApi, COMPOSE_GRAPH } from './fixtures/api';
import type { AppModel } from '../src/types/app';
import type { SourceConnection, TriggerIntent, TriggerPreview, TriggerRule } from '../src/api/automation';

// Browser/API-contract evidence only: every request is fulfilled locally.
// Provider authentication, secret encryption and deployment execution have
// separate backend/live acceptance tests; no real token or repository is used.
const TOKEN = 'browser-fixture-repository-token';
const WEBHOOK_SECRET = 'browser-fixture-webhook-secret-00000000';
const COMMIT = 'a'.repeat(40);
const CONNECTION: SourceConnection = {
  id: 'fixture-connection', name: 'Team Git', provider: 'github', origin: 'https://github.com',
  apiOrigin: 'https://api.github.com', enabled: true, version: 1, repositories: ['team/shop'],
  authMode: 'github_app', installationId: 42, webhookAuthMode: 'signing', hasToken: false, hasWebhookSecret: true,
};
const APP: AppModel = {
  id: 'fixture-app', name: 'Shop', githubRepo: 'team/shop', branch: 'main', version: 7,
  buildPlan: { kind: 'compose', files: ['compose.yaml'], environmentScope: 'service', serviceEnvironment: { web: ['DB_PASSWORD'], worker: ['DB_PASSWORD'] } },
  deployTarget: { kind: 'docker-host', prefix: 'shop-fixture' }, environmentId: 'fixture-environment',
  hasWebhook: false, autoDeploy: false, canary: { strategy: 'rolling' },
  createdAt: '2026-09-28T00:00:00Z', updatedAt: '2026-09-28T00:00:00Z',
};

type RuleRequest = { rule: TriggerRule; ref: string; expectedEnvelopeHash?: string; expectedPlanHash?: string };

async function setup(page: Page, options: { legacyScope?: boolean; intents?: TriggerIntent[]; emptyConnections?: boolean } = {}) {
  await mockApi(page);
  let app = structuredClone(APP);
  if (options.legacyScope) app.buildPlan = { kind: 'compose', files: ['compose.yaml'] };
  const connections: SourceConnection[] = options.emptyConnections ? [] : [structuredClone(CONNECTION)];
  const rules: TriggerRule[] = [];
  const intents = structuredClone(options.intents ?? []);
  const connectionWrites: Record<string, unknown>[] = [];
  const appWrites: AppModel[] = [];
  const previews: { request: RuleRequest; response: TriggerPreview }[] = [];
  const saves: RuleRequest[] = [];
  const approvals: string[] = [];
  const unexpected: string[] = [];

  await page.route('**/api/v1/admin/**', async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.replace('/api/v1/admin', '');
    if (path.startsWith('/integrations/vercel/')) return route.fallback();
    if (path === '/notification-deliveries' && req.method() === 'GET') return route.fallback();
    if (path === '/source-connections') {
      if (req.method() === 'GET') return route.fulfill({ json: connections });
      const body = req.postDataJSON() as Record<string, unknown>;
      connectionWrites.push(body);
      // Deliberately return the real API's public projection, never the
      // submitted token/webhook secret, so the listing can prove its contract.
      const item: SourceConnection = {
        ...CONNECTION, id: `created-connection-${connectionWrites.length}`, name: String(body.name),
        provider: body.provider as SourceConnection['provider'], origin: String(body.origin),
        apiOrigin: String(body.apiOrigin), repositories: body.repositories as string[],
        authMode: String(body.authMode), installationId: body.installationId as number | undefined,
        webhookAuthMode: String(body.webhookAuthMode), hasToken: !!body.token,
      };
      connections.push(item);
      return route.fulfill({ status: 201, json: item });
    }
    if (path === '/trigger-rules/preview') {
      const request = req.postDataJSON() as RuleRequest;
      const index = previews.length + 1;
      const response: TriggerPreview = {
        rule: {
          ...request.rule, id: request.rule.id || 'reviewed-rule', source: { ...request.rule.source, repositoryId: 'server-repository-id' },
          planHash: String(index).repeat(64), approvedEnvelopeHash: String(index + 4).repeat(64),
        },
        envelope: { hash: String(index + 4).repeat(64), commit: COMMIT, files: ['compose.yaml', 'Dockerfile'], graph: COMPOSE_GRAPH },
      };
      previews.push({ request, response });
      return route.fulfill({ json: response });
    }
    if (path === '/trigger-rules') {
      if (req.method() === 'GET') return route.fulfill({ json: rules });
      const body = req.postDataJSON() as RuleRequest;
      saves.push(body);
      const latest = previews.at(-1)?.response;
      if (!latest || body.expectedEnvelopeHash !== latest.envelope.hash || body.expectedPlanHash !== latest.rule.planHash) {
        return route.fulfill({ status: 409, json: { error: 'configuration changed; preview again' } });
      }
      const saved = { ...body.rule, version: body.rule.version + 1 };
      rules.push(saved);
      return route.fulfill({ json: saved });
    }
    if (path === '/trigger-intents') return route.fulfill({ json: intents });
    if (path === '/trigger-deliveries') {
      const items = intents.map((i, index) => ({ deliveryId: `delivery-${index}`, providerDeliveryId: `provider-${index}`, provider: 'github', connectionId: CONNECTION.id, repository: 'team/shop', fullRef: i.event.fullRef, commitSha: i.event.commitSha, deleted: false, ruleId: i.rule.id, ruleVersion: i.rule.version, appId: APP.id, targetSource: 'intent', intentId: i.id, intentStatus: i.status, jobId: i.receipt ? 'queued-job' : undefined, pipelineId: i.receipt?.pipelineId, runId: i.receipt?.runId, status: i.status === 'awaiting_review' ? 'review_required' : 'accepted', reasonCode: i.status === 'queued' ? 'queued' : 'accepted', reason: i.status === 'queued' ? 'Deployment durably queued. Queue acceptance does not confirm a successful rollout.' : 'Intent recorded; source, actor and deployment configuration still require validation. Deployment has not started.', createdAt: '2026-10-02T00:00:00Z', updatedAt: '2026-10-02T00:00:00Z', archived: false, decisionIndex: 1 }));
      return route.fulfill({ json: { items, total: items.length, limit: 25, offset: 0, coverage: 'authenticated_admitted_decisions', limitations: [] } });
    }
    const approval = path.match(/^\/trigger-intents\/([^/]+)\/approve$/);
    if (approval) {
      approvals.push(approval[1]);
      const item = intents.find((i) => i.id === approval[1]);
      if (item) { item.status = 'pending'; item.reason = 'approved; awaiting final source checks'; }
      return route.fulfill({ status: 202, json: { intentId: approval[1], status: 'pending' } });
    }
    unexpected.push(`${req.method()} ${path}`);
    return route.fulfill({ status: 404, json: { error: `Unexpected automation fixture: ${path}` } });
  });
  await page.route('**/api/v1/apps**', async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace('/api/v1', '');
    if (path === '/apps/fixture-app') {
      if (req.method() === 'PUT') {
        const body = req.postDataJSON() as AppModel;
        appWrites.push(body);
        app = { ...body, version: (app.version ?? 0) + 1 };
      }
      return route.fulfill({ json: app });
    }
    if (path.endsWith('/deploys')) return route.fulfill({ json: { deploys: [] } });
    if (path.endsWith('/drift')) return route.fulfill({ json: { status: 'unknown' } });
    if (path === '/apps') return route.fulfill({ json: [app] });
    unexpected.push(`${req.method()} ${path}`);
    return route.fulfill({ status: 404, json: { error: `Unexpected app fixture: ${path}` } });
  });
  return { connections, connectionWrites, appWrites, previews, saves, approvals, unexpected, app: () => app };
}

for (const provider of ['gitea', 'gitlab'] as const) {
  test(`switches GitHub App installation to ${provider} and clears credentials after creating the connection`, async ({ page }) => {
    const fixture = await setup(page, { emptyConnections: true });
    await page.goto('/settings');
    await page.getByRole('button', { name: 'Connect source', exact: true }).click();
    await page.getByLabel('Connection name', { exact: false }).fill(`${provider} fixture`);
    await page.getByLabel('Allowed repositories', { exact: false }).fill('team/shop');
    await page.getByLabel('Webhook secret', { exact: false }).fill('old-provider-secret-must-not-survive');
    await page.getByLabel('GitHub App installation ID', { exact: false }).fill('42');
    await expect(page.getByLabel('Read-only repository token', { exact: false })).toHaveCount(0);
    await page.getByLabel('Git provider', { exact: false }).selectOption(provider);
    await expect(page.getByLabel('Read-only repository token', { exact: false })).toBeVisible();
    await expect(page.getByLabel('Read-only repository token', { exact: false })).toHaveValue('');
    await page.getByLabel('Server address', { exact: false }).fill(`https://${provider}.example.test`);
    await page.getByLabel('Read-only repository token', { exact: false }).fill(TOKEN);
    await page.getByLabel('Git username', { exact: false }).fill('fixture-user');
    if (provider === 'gitlab') await page.getByLabel('Webhook authentication', { exact: false }).selectOption('legacy_token');
    await expect(page.getByLabel('Webhook secret', { exact: false })).toHaveValue('');
    await page.getByLabel('Webhook secret', { exact: false }).fill(WEBHOOK_SECRET);
    await page.getByRole('button', { name: 'Save connection', exact: true }).click();
    await expect(page.getByText(`${provider} fixture`, { exact: true })).toBeVisible();
    expect(fixture.connectionWrites).toHaveLength(1);
    expect(fixture.connectionWrites[0]).toMatchObject({ provider, authMode: 'token', token: TOKEN, username: 'fixture-user', webhookSecret: WEBHOOK_SECRET });
    expect(fixture.connectionWrites[0]).not.toHaveProperty('installationId');
    await expect(page.getByText('/webhooks/connections/created-connection-1', { exact: false })).toBeVisible();
    await expect(page.locator('body')).not.toContainText(TOKEN);
    await expect(page.locator('body')).not.toContainText(WEBHOOK_SECRET);
    await page.getByRole('button', { name: 'Connect source', exact: true }).click();
    await expect(page.getByLabel('Read-only repository token', { exact: false })).toHaveValue('');
    await expect(page.getByLabel('Webhook secret', { exact: false })).toHaveValue('');
    expect(fixture.unexpected).toEqual([]);
  });
}

test('GitHub App authentication omits a previously entered repository token and username', async ({ page }) => {
  const fixture = await setup(page, { emptyConnections: true });
  await page.goto('/settings');
  await page.getByRole('button', { name: 'Connect source', exact: true }).click();
  await page.getByLabel('Connection name', { exact: false }).fill('Installation fixture');
  await page.getByLabel('Allowed repositories', { exact: false }).fill('team/shop');
  await page.getByLabel('Read-only repository token', { exact: false }).fill(TOKEN);
  await page.getByLabel('Git username', { exact: false }).fill('stale-token-user');
  await page.getByLabel('GitHub App installation ID', { exact: false }).fill('42');
  await expect(page.getByLabel('Read-only repository token', { exact: false })).toHaveCount(0);
  await page.getByLabel('Webhook secret', { exact: false }).fill(WEBHOOK_SECRET);
  await page.getByRole('button', { name: 'Save connection', exact: true }).click();
  await expect(page.getByText('Installation fixture', { exact: true })).toBeVisible();
  expect(fixture.connectionWrites[0]).toMatchObject({ provider: 'github', authMode: 'github_app', installationId: 42 });
  expect(fixture.connectionWrites[0]).not.toHaveProperty('token');
  expect(fixture.connectionWrites[0]).not.toHaveProperty('username');
  expect(fixture.unexpected).toEqual([]);
});

async function openRule(page: Page) {
  await page.goto('/apps/fixture-app');
  await expect(page.getByRole('heading', { name: 'Git deployment rules', exact: true })).toBeVisible();
  await page.getByLabel('Source connection', { exact: false }).selectOption(CONNECTION.id);
}

test('previews a tag rule and submits the server-issued plan and envelope hashes when saving', async ({ page }) => {
  const fixture = await setup(page);
  await openRule(page);
  await page.getByRole('combobox', { name: 'Watch', exact: true }).selectOption('tag');
  await page.getByRole('combobox', { name: 'Match', exact: true }).selectOption('glob');
  await page.getByLabel('Include', { exact: false }).fill('v*');
  await page.getByLabel('Exclude', { exact: false }).fill('v*-rc*');
  await page.getByLabel('Tag to inspect now', { exact: false }).fill('v1.2.3');
  await page.getByLabel('Deployment approval', { exact: false }).selectOption('automatic');
  await page.getByRole('button', { name: 'Preview rule', exact: true }).click();
  await expect(page.getByText(`Reviewed commit ${COMMIT.slice(0, 12)}`, { exact: false })).toBeVisible();
  expect(fixture.previews[0].request).toMatchObject({ ref: 'refs/tags/v1.2.3', rule: { appId: APP.id, refKind: 'tag', match: 'glob', include: ['v*'], exclude: ['v*-rc*'], approval: 'automatic' } });
  await page.getByRole('button', { name: 'Approve configuration & save rule', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Edit & review', exact: true })).toBeVisible();
  expect(fixture.saves).toHaveLength(1);
  expect(fixture.saves[0]).toEqual({ rule: fixture.previews[0].response.rule, ref: 'refs/tags/v1.2.3', expectedEnvelopeHash: fixture.previews[0].response.envelope.hash, expectedPlanHash: fixture.previews[0].response.rule.planHash });
  await expect(page.getByRole('button', { name: 'Approve configuration & save rule', exact: true })).toHaveCount(0);
  expect(fixture.unexpected).toEqual([]);
});

test('changing the ref or service permissions invalidates the previous preview', async ({ page }) => {
  const fixture = await setup(page);
  await openRule(page);
  await page.getByRole('button', { name: 'Preview rule', exact: true }).click();
  const approve = page.getByRole('button', { name: 'Approve configuration & save rule', exact: true });
  await expect(approve).toBeVisible();
  await page.getByLabel('Branch to inspect now', { exact: false }).fill('release');
  await expect(approve).toHaveCount(0);
  await page.getByLabel('Include', { exact: false }).fill('release');
  await page.getByRole('button', { name: 'Preview rule', exact: true }).click();
  await expect(approve).toBeVisible();
  expect(fixture.previews[1].request.ref).toBe('refs/heads/release');
  expect(fixture.previews[1].response.envelope.hash).not.toBe(fixture.previews[0].response.envelope.hash);
  await page.getByText('Configuration for each service', { exact: true }).click();
  await page.getByLabel('Service permissions', { exact: false }).fill('web: DB_PASSWORD, API_KEY');
  await expect(approve).toHaveCount(0);
  expect(fixture.saves).toEqual([]);
  expect(fixture.unexpected).toEqual([]);
});

test('saves explicit service permissions, preserves App version, and supports deny-all', async ({ page }) => {
  const fixture = await setup(page, { legacyScope: true });
  await openRule(page);
  await expect(page.getByRole('button', { name: 'Preview rule', exact: true })).toBeDisabled();
  const permissions = page.getByLabel('Service permissions', { exact: false });
  await permissions.fill('web: DB_PASSWORD\nweb: API_KEY');
  await page.getByRole('button', { name: 'Save service permissions', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Service web appears more than once.');
  expect(fixture.appWrites).toEqual([]);
  await permissions.fill('web: DB_PASSWORD, API_KEY\nworker: DB_PASSWORD\nredis:');
  await page.getByRole('button', { name: 'Save service permissions', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Preview rule', exact: true })).toBeEnabled();
  expect(fixture.appWrites[0]).toMatchObject({ id: APP.id, version: 7, autoDeploy: false, environmentId: APP.environmentId, buildPlan: { kind: 'compose', files: ['compose.yaml'], environmentScope: 'service', serviceEnvironment: { web: ['DB_PASSWORD', 'API_KEY'], worker: ['DB_PASSWORD'], redis: [] } } });
  expect(fixture.app().version).toBe(8);
  await page.getByText('Configuration for each service', { exact: true }).click();
  await expect(permissions).toHaveValue('web: DB_PASSWORD, API_KEY\nworker: DB_PASSWORD\nredis: ');
  await permissions.fill('');
  await page.getByRole('button', { name: 'Save service permissions', exact: true }).click();
  await expect.poll(() => fixture.appWrites.length).toBe(2);
  expect(fixture.appWrites[1]).toMatchObject({ version: 8, buildPlan: { environmentScope: 'service', serviceEnvironment: {} } });
  expect(fixture.unexpected).toEqual([]);
});

test('review inbox approves only the selected intent and links queued deployment receipts', async ({ page }) => {
  const rule: TriggerRule = { id: 'rule', version: 1, name: 'Shop branch', enabled: true, appId: APP.id, source: { connectionId: CONNECTION.id }, refKind: 'branch', match: 'exact', include: ['main'], exclude: [], approval: 'review' };
  const fixture = await setup(page, { intents: [
    { id: 'review-intent', status: 'awaiting_review', reason: 'review required', rule, event: { fullRef: 'refs/heads/main', commitSha: COMMIT } },
    { id: 'queued-intent', status: 'queued', reason: 'accepted', rule, event: { fullRef: 'refs/heads/main', commitSha: 'b'.repeat(40) }, receipt: { pipelineId: 'queued-pipeline', runId: 'queued-run' } },
  ] });
  await openRule(page);
  await page.getByRole('article', { name: 'Accepted delivery decision' }).getByText('Inspect delivery, intent and job', { exact: true }).click();
  await expect(page.getByRole('link', { name: 'Open linked run', exact: true })).toHaveAttribute('href', '/apps/fixture-app/deployments/queued-pipeline/queued-run');
  await page.getByRole('button', { name: 'Approve this commit', exact: true }).click();
  await expect(page.getByRole('article', { name: 'Accepted delivery decision' })).toHaveCount(2);
  await expect(page.getByRole('button', { name: 'Approve this commit', exact: true })).toHaveCount(0);
  expect(fixture.approvals).toEqual(['review-intent']);
  expect(fixture.unexpected).toEqual([]);
});
