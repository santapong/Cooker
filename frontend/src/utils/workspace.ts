import type { AppModel } from '../types/app';
import type { RunStatus } from '../types/pipeline';
import type { BadgeVariant } from '../components/ui/Badge';

export function appHealth(app: Pick<AppModel, 'healthStatus'>): { label: string; variant: BadgeVariant } {
  switch (app.healthStatus) {
    case 'healthy': return { label: 'Healthy', variant: 'ok' };
    case 'degraded': return { label: 'Degraded', variant: 'ember' };
    case 'failed': return { label: 'Failed', variant: 'fail' };
    default: return { label: 'Health not checked', variant: 'muted' };
  }
}

export function runState(status: RunStatus): { label: string; variant: BadgeVariant } {
  switch (status) {
    case 'pending': return { label: 'Queued', variant: 'muted' };
    case 'running': return { label: 'Running', variant: 'running' };
    case 'needs_attention': return { label: 'Needs attention', variant: 'ember' };
    case 'success': return { label: 'Succeeded', variant: 'ok' };
    case 'failed': return { label: 'Failed', variant: 'fail' };
    case 'cancelled': return { label: 'Cancelled', variant: 'muted' };
    case 'skipped': return { label: 'Skipped', variant: 'muted' };
    default: return { label: 'Unknown', variant: 'muted' };
  }
}

export function appURL(value?: string): string | undefined {
  if (!value) return undefined;
  try {
    const url = new URL(value);
    if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) return undefined;
    return url.href;
  } catch { return undefined; }
}

export const workspaceError = (error: unknown) => error instanceof Error ? error.message : String(error);
