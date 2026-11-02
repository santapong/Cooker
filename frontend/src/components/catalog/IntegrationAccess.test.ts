import { createElement, type ComponentProps } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { IntegrationInstallation, IntegrationInventory } from '../../types/integrations';
import IntegrationAccess from './IntegrationAccess';

const observedAt = '2026-10-25T01:00:00Z';
const attemptedAt = '2026-10-25T02:00:00Z';

function installation(overrides: Partial<IntegrationInstallation> = {}): IntegrationInstallation {
  return {
    id: 'icfg_neon', integrationId: 'oac_neon', name: 'Neon', slug: 'neon',
    installationType: 'marketplace', providerStatus: 'ready',
    projectAccess: 'selected_projects', projectIds: ['prj_selected'],
    ...overrides,
  };
}

function inventory(overrides: Partial<IntegrationInventory> = {}): IntegrationInventory {
  return { items: [installation()], observedAt, attemptedAt, state: 'available', complete: true, ...overrides };
}

function render(props: ComponentProps<typeof IntegrationAccess> = {}) {
  return renderToStaticMarkup(createElement(IntegrationAccess, props));
}

function clock(html: string, label: string) {
  return html.match(new RegExp(`${label}: <time\\b[^>]*>[^<]*<\\/time>`, 'i'))?.[0];
}

describe('read-only integration installation observations', () => {
  it.each([undefined, null])('keeps a legacy missing inventory unknown: %s', (value) => {
    const html = render({ inventory: value });
    expect(html).toContain('Not observed');
    expect(html).toContain('Installation count is unknown.');
    expect(html).not.toContain('reported no installations');
    expect(html).not.toContain('Integration inventory observed:');
    expect(html).not.toContain('role="listitem"');
  });

  it.each([
    ['unauthorized', 'authentication'],
    ['forbidden', 'Access denied'],
  ])('does not present a first %s failure as zero installations', (errorCode, explanation) => {
    const html = render({ inventory: inventory({
      items: [], observedAt: undefined, state: 'unavailable', complete: false, errorCode,
    }) });
    expect(html).toContain('Unavailable');
    expect(html).toContain('Installation count is unknown.');
    expect(html).toContain(explanation);
    expect(html).toContain('role="status"');
    expect(clock(html, 'Integration inventory last attempted')).toContain(`"${attemptedAt}"`);
    expect(clock(html, 'Integration inventory observed')).toBeUndefined();
    expect(html).not.toContain('reported no installations');
    expect(html).not.toContain('Complete installation observation');
  });

  it.each([
    { complete: false },
    { observedAt: undefined },
    { observedAt: 'invalid-clock' },
  ])('requires complete inventory and its own valid observation clock: %j', (overrides) => {
    const html = render({ inventory: inventory(overrides) });
    expect(html).toContain('Not observed');
    expect(html).toContain('Installation count is unknown.');
    expect(html).not.toContain('role="listitem"');
    expect(html).not.toContain('reported no installations');
    expect(clock(html, 'Integration inventory observed')).toBeUndefined();
    expect(clock(html, 'Integration inventory last attempted')).toContain(`"${attemptedAt}"`);
  });

  it('retains stale records with the successful clock, attempted clock and refresh explanation', () => {
    const html = render({ inventory: inventory({
      state: 'stale', errorCode: 'rate_limited', error: 'Cooker could not refresh installation inventory.',
    }) });
    expect(html).toContain('Stale');
    expect(html).toContain('<h3>Neon</h3>');
    expect(html).toContain('Showing the last successful observation.');
    expect(html).toContain('These installation records have not been freshly confirmed.');
    expect(html).toContain('Cooker could not refresh installation inventory.');
    const successfulClock = clock(html, 'Integration inventory observed');
    const attemptClock = clock(html, 'Integration inventory last attempted');
    expect(successfulClock).toContain(`"${observedAt}"`);
    expect(successfulClock).not.toContain(`"${attemptedAt}"`);
    expect(attemptClock).toContain(`"${attemptedAt}"`);
    expect(attemptClock).not.toContain(`"${observedAt}"`);
  });

  it('distinguishes a successful empty project observation from an old empty observation', () => {
    const current = render({ inventory: inventory({ items: [] }) });
    expect(current).toContain('Available');
    expect(current).toContain('Vercel reported no installations with allowed access to this project.');
    expect(current).not.toContain('last successful observation');
    expect(current).not.toContain('Installation count is unknown.');
    expect(clock(current, 'Integration inventory observed')).toContain(`"${observedAt}"`);

    const stale = render({ inventory: inventory({ items: [], state: 'stale' }), scope: 'connection' });
    expect(stale).toContain('Stale');
    expect(stale).toContain('The last successful observation reported no installations with allowed access to this connection’s selected projects.');
    expect(stale).toContain('have not been freshly confirmed');
    expect(stale).not.toContain('Vercel reported no installations');
    expect(clock(stale, 'Integration inventory observed')).toContain(`"${observedAt}"`);
    expect(clock(stale, 'Integration inventory last attempted')).toContain(`"${attemptedAt}"`);
  });

  it.each([
    ['marketplace', 'Marketplace installation'],
    ['external', 'External installation'],
    ['future_installation', 'Unknown installation type'],
  ])('renders %s installation metadata without treating it as a service', (installationType, label) => {
    const html = render({ inventory: inventory({ items: [installation({ installationType })] }) });
    expect(html).toContain(label);
    expect(html).toContain('Provider-reported installation state: Ready');
    expect(html).toContain('Installation state does not measure health');
    expect(html).toContain('a reported Neon installation does not verify SQL connectivity');
    expect(html).not.toMatch(/badge-(?:ok|running|fail)/);
    expect(html).not.toContain('Database connected');
  });

  it('keeps all-project permission distinct from the IDs visible in the selected scope', () => {
    const html = render({
      inventory: inventory({ items: [installation({ projectAccess: 'all_projects', projectIds: ['prj_selected', 'prj_selected'] })] }),
      projects: [{ id: 'prj_selected', name: 'Selected app' }, { id: 'prj_other', name: 'Unselected app' }],
    });
    expect(html).toContain('Allowed project access:');
    expect(html).toContain('All projects (provider permission)');
    expect(html).toContain('Project IDs visible in this selected scope:');
    expect(html).toContain('Selected app');
    expect(html.match(/<code>prj_selected<\/code>/g)).toHaveLength(1);
    expect(html).not.toContain('prj_other');
    expect(html).not.toContain('Unselected app');
    expect(html).toContain('It does not identify resource or database bindings.');
  });

  it('preserves selected permission with no listed project IDs without claiming empty installation inventory', () => {
    const html = render({ inventory: inventory({ items: [installation({ projectIds: [] })] }) });
    expect(html).toContain('Selected projects (provider permission)');
    expect(html).toContain('No project IDs listed in this selected scope.');
    expect(html).toContain('role="listitem"');
    expect(html).not.toContain('reported no installations');
  });

  it.each(['__proto__', 'constructor', 'toString'])('treats prototype-like provider enums as unknown: %s', (value) => {
    const html = render({ inventory: inventory({
      state: value, errorCode: value,
      items: [installation({ installationType: value, providerStatus: value, projectAccess: value })],
    }) });
    expect(html).toContain('Observation state unknown');
    expect(html).toContain('Unknown installation type');
    expect(html).toContain('Provider-reported installation state: Unknown');
    expect(html).toContain('Unknown project access');
    expect(html).toContain('have not been freshly confirmed');
    expect(html).not.toContain('role="status"');
    expect(html).not.toContain('[object Object]');
  });

  it('escapes provider names, identities, project labels and errors as text with no provider navigation', () => {
    const html = render({
      inventory: inventory({
        error: '<script>alert("inventory")</script>',
        items: [installation({
          name: '<img src=x onerror="alert(1)">', id: '<installation>', integrationId: '<integration>',
          projectIds: ['<project>'],
        })],
      }),
      projects: [{ id: '<project>', name: '<svg onload="alert(2)">' }],
    });
    expect(html).toContain('&lt;img src=x onerror=&quot;alert(1)&quot;&gt;');
    expect(html).toContain('&lt;installation&gt;');
    expect(html).toContain('&lt;integration&gt;');
    expect(html).toContain('&lt;project&gt;');
    expect(html).toContain('&lt;svg onload=&quot;alert(2)&quot;&gt;');
    expect(html).toContain('&lt;script&gt;alert(&quot;inventory&quot;)&lt;/script&gt;');
    expect(html).not.toMatch(/<(?:img|script|svg|a)\b/);
    expect(html).not.toMatch(/\bhref=/);
  });

  it('keeps each compact connection observation separate and exposes no new action controls or links', () => {
    const html = renderToStaticMarkup(createElement('div', null,
      createElement(IntegrationAccess, { inventory: inventory(), compact: true, scope: 'connection' }),
      createElement(IntegrationAccess, { inventory: inventory(), compact: true, scope: 'connection' }),
    ));
    expect(html.match(/View 1 installation record for this connection/g)).toHaveLength(2);
    expect(html.match(/role="listitem"/g)).toHaveLength(2);
    expect(html).toContain('Read-only installation metadata.');
    expect(html).toContain('Resource bindings, health, and SQL connectivity are not checked.');
    expect(html).toContain('<details><summary>');
    expect(html).not.toContain('2 installations');
    expect(html).not.toMatch(/<(?:button|input|select|textarea|form|a)\b/);
    expect(html).not.toMatch(/\bhref=/);
    expect(html).not.toMatch(/badge-(?:ok|running|fail)/);
  });
});
