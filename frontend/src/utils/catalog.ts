import type { CatalogApp, CatalogPipeline, CatalogService } from '../types/catalog';
/** Internal catalog navigation never accepts an off-origin or protocol-relative href. */
export function catalogHref(value: string | undefined, fallback: string): string {
  return value && /^\/(?:apps|pipelines|services)(?:\/|\?|$)/.test(value) && !/[\\\r\n]/.test(value) ? value : fallback;
}
export const catalogAppHref = (app: CatalogApp) => app.management === 'vercel' ? '/apps/synced/' + encodeURIComponent(app.id) : catalogHref(app.href, '/apps');
export const catalogPipelineHref = (pipeline: CatalogPipeline) => pipeline.kind === 'provider-flow' ? '/pipelines/synced/' + encodeURIComponent(pipeline.id) : catalogHref(pipeline.href, '/pipelines');
/** Inspector links originate in the provider response, and stay on Vercel's HTTPS UI. */
export function vercelInspector(value?: string): string | undefined {
  try { const u = new URL(value ?? ''); return u.protocol === 'https:' && u.hostname === 'vercel.com' && !u.username && !u.password && !u.port ? u.href : undefined; } catch { return undefined; }
}
export function deploymentURL(value?: string): string | undefined {
  try { const u = new URL(value ?? ''); return u.protocol === 'https:' && !u.username && !u.password ? u.href : undefined; } catch { return undefined; }
}
export function deploymentLabel(value?: string): string {
  const state = value?.toLowerCase();
  if (state === 'ready') return 'Deployment ready';
  if (state === 'success') return 'Deployment succeeded';
  if (state === 'error' || state === 'failed') return 'Deployment failed';
  if (state === 'canceled' || state === 'cancelled') return 'Deployment cancelled';
  return value ? value.replace(/_/g, ' ') : 'Deployment not recorded';
}
export function healthLabel(value?: string): string { return !value || ['unknown', 'unchecked', 'not_checked'].includes(value.toLowerCase()) ? 'Health not checked' : value.replace(/_/g, ' '); }
export function groupServices(services: CatalogService[]): { label: string; items: CatalogService[] }[] {
  const groups = new Map<string, CatalogService[]>();
  const previewDeployments = new Map<string, string>();
  // Keep all services sharing the newest preview deployment, rather than one service per app.
  const sorted = [...services].sort((a, b) => (b.observedAt ?? '').localeCompare(a.observedAt ?? ''));
  for (const service of sorted) {
    const env = service.environment?.toLowerCase();
    const preview = service.management === 'vercel' && env === 'preview';
    if (preview && service.deploymentId) {
      const known = previewDeployments.get(service.appId);
      if (known && known !== service.deploymentId) continue;
      previewDeployments.set(service.appId, service.deploymentId);
    }
    const label = service.management === 'vercel' && env === 'production' ? 'Production' : preview ? 'Latest previews' : 'Configured, recorded, or unclassified';
    groups.set(label, [...(groups.get(label) ?? []), service]);
  }
  return ['Production', 'Latest previews', 'Configured, recorded, or unclassified'].flatMap((label) => groups.has(label) ? [{ label, items: groups.get(label)! }] : []);
}
