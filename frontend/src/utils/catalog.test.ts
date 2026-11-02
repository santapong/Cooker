import { describe, expect, it } from 'vitest';
import { catalogAppHref, catalogHref, deploymentLabel, deploymentURL, groupServices, healthLabel, vercelInspector } from './catalog';
import type { CatalogApp, CatalogService } from '../types/catalog';
describe('catalog trust and ownership boundaries', () => {
  it('never lets catalog navigation redirect to another origin', () => {
    for (const href of ['https://evil.test', '//evil.test', '/\\evil.test', 'javascript:alert(1)', '/settings']) expect(catalogHref(href, '/apps')).toBe('/apps');
    expect(catalogHref('/apps/native-1', '/apps')).toBe('/apps/native-1');
  });
  it('always routes imported apps to read-only detail regardless of supplied native href', () => {
    expect(catalogAppHref({ id: 'vapp_123', management: 'vercel', href: '/apps/native/edit' } as CatalogApp)).toBe('/apps/synced/vapp_123');
  });
  it('only accepts Vercel HTTPS inspector links without credentials or custom ports', () => {
    expect(vercelInspector('https://vercel.com/team/project/deployment')).toBe('https://vercel.com/team/project/deployment');
    for (const url of ['http://vercel.com/x', 'https://vercel.com.evil.test/x', 'https://user:secret@vercel.com/x', 'https://vercel.com:8443/x', 'javascript:alert(1)']) expect(vercelInspector(url)).toBeUndefined();
  });
  it('keeps deployment URLs distinct from provider inspector origins', () => {
    expect(deploymentURL('https://product.example.com')).toBe('https://product.example.com/');
    expect(deploymentURL('https://user:secret@product.example.com')).toBeUndefined();
    expect(deploymentURL('data:text/html,hi')).toBeUndefined();
  });
  it('never maps READY or success into a health verdict', () => {
    expect(deploymentLabel('READY')).toBe('Deployment ready');
    expect(healthLabel('unknown')).toBe('Health not checked');
    expect(healthLabel()).toBe('Health not checked');
  });
});
describe('service scope', () => {
  const service = (id: string, overrides: Partial<CatalogService>): CatalogService => ({ id, appId: 'a', appName: 'App', appHref: '/apps/synced/a', name: id, provider: 'vercel', management: 'vercel', environment: 'preview', status: 'ready', health: 'unknown', definition: 'deployed', ...overrides });
  it('keeps all services sharing latest preview and does not guess native production from its name', () => {
    const groups = groupServices([service('old', { deploymentId: 'old', observedAt: '2026-09-01' }), service('new-a', { deploymentId: 'new', observedAt: '2026-10-01' }), service('new-b', { deploymentId: 'new', observedAt: '2026-10-01' }), service('prod', { environment: 'production' }), service('native', { management: 'cooker', environment: 'production', definition: 'recorded' })]);
    expect(groups.map((g) => g.label)).toEqual(['Production', 'Latest previews', 'Configured, recorded, or unclassified']);
    expect(groups[1].items.map((s) => s.id)).toEqual(['new-a', 'new-b']);
    expect(groups[2].items[0].id).toBe('native');
  });
});
