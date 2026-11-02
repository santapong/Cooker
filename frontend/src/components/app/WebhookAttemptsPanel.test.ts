import { createElement, type ReactElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { StaticRouter } from 'react-router-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Pipeline } from '../../types/pipeline';
import type { WebhookAttempt } from '../../api/triggerHistory';

const auth = vi.hoisted(() => ({ roles: ['admin'] }));
vi.mock('../../auth/OIDCProvider', () => ({ useAuth: () => ({ user: { roles: auth.roles } }) }));
vi.mock('../../api/client', () => ({ get: vi.fn(), post: vi.fn() }));

import NativeAutomationPanel from '../pipeline/NativeAutomationPanel';
import TriggerHistoryPanel from './TriggerHistoryPanel';
import WebhookAttemptsPanel, { WebhookAttemptRows } from './WebhookAttemptsPanel';

function render(element: ReactElement) {
  return renderToStaticMarkup(createElement(StaticRouter, { location: '/pipelines/pipeline1/edit' }, element));
}
const pipeline: Pipeline = {
  id: 'pipeline1', name: 'Website', description: '', version: 2,
  stages: [], edges: [], variables: {}, createdAt: '', updatedAt: '',
  source: { connectionId: 'source1', repository: 'team/website', fullRef: 'refs/heads/main' },
};
const attempt = (id: string, status: WebhookAttempt['status'], reasonCode: string): WebhookAttempt => ({
  id, status, reasonCode, provider: 'gitea', connectionId: 'source1',
  createdAt: '2026-10-05T00:00:00Z', finishedAt: '2026-10-05T00:00:01Z',
});

afterEach(() => { auth.roles = ['admin']; });

describe('native authenticated request visibility', () => {
  it('exposes incoming attempts from the actual native source binding', () => {
    expect(render(createElement(NativeAutomationPanel, { pipeline, dirty: false }))).toContain('Incoming webhook attempts');
    const html = render(createElement(WebhookAttemptsPanel, { connectionId: pipeline.source!.connectionId, onInspect: () => {} }));
    expect(html).toContain('<option value="connection" selected="">Requests for this source connection</option>');
    expect(html).toContain('This source connection may serve several pipelines.');
    expect(html).toContain('Requests without a delivery decision are not assigned to this pipeline.');
    expect(html).not.toContain('Requests linked to this App');
  });

  it('keeps a native pipeline without source unavailable and preserves the administrator gate', () => {
    expect(render(createElement(NativeAutomationPanel, { pipeline: { ...pipeline, source: null }, dirty: false }))).not.toContain('Incoming webhook attempts');
    auth.roles = ['operator'];
    const html = render(createElement(NativeAutomationPanel, { pipeline, dirty: false }));
    expect(html).toContain('An administrator can configure');
    expect(html).not.toContain('Incoming webhook attempts');
  });

  it('preserves the App entry and all-request default without a source connection', () => {
    expect(render(createElement(TriggerHistoryPanel, { appId: 'app1' }))).toContain('Incoming webhook attempts');
    const html = render(createElement(WebhookAttemptsPanel, { appId: 'app1', onInspect: () => {} }));
    expect(html).toContain('<option value="all" selected="">All incoming requests</option>');
    expect(html).toContain('Requests linked to this App');
    expect(html).not.toContain('Requests for this source connection');
  });

  it('shows authenticated rejection and uncertainty before inbox admission without inventing delivery links', () => {
    const html = render(createElement(WebhookAttemptRows, {
      items: [attempt('rejected1', 'rejected', 'invalid_json'), attempt('uncertain1', 'uncertain', 'admission_unconfirmed')],
      deliveryHref: (id) => `?gitDelivery=${encodeURIComponent(id)}`, onInspect: () => {},
    }));
    expect(html).toContain('Rejected request');
    expect(html).toContain('The authenticated request was not valid JSON.');
    expect(html).toContain('Inbox admission unconfirmed');
    expect(html).toContain('Inbox admission could not be confirmed.');
    expect(html).toContain('rejected1');
    expect(html).toContain('uncertain1');
    expect(html).not.toContain('href=');
    expect(html).not.toContain('deployment succeeded');
  });

  it('links a real retained duplicate to its delivery and leaves an unassigned failed request unlinked', () => {
    const html = render(createElement(WebhookAttemptRows, {
      items: [{ ...attempt('duplicate1', 'duplicate', 'already_recorded'), deliveryId: 'delivery/one' }, attempt('failed1', 'failed', 'history_unavailable')],
      deliveryHref: (id) => `?gitDelivery=${encodeURIComponent(id)}`, onInspect: () => {},
    }));
    expect(html).toContain('Authenticated retry');
    expect(html).toContain('It did not re-admit a deployment intent.');
    expect(html).toContain('href="/pipelines/pipeline1/edit?gitDelivery=delivery%2Fone"');
    expect(html.match(/Open matching delivery decisions/g)).toHaveLength(1);
    expect(html).toContain('Request processing failed');
    expect(html).toContain('The durable inbox could not be read.');
  });
});
