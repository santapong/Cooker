import type { VercelConnection } from '../types/vercel';
import type { BadgeVariant } from '../components/ui/Badge';

/** Provider URLs never become executable, credential-bearing or relative links. */
export function vercelDeploymentURL(value: string): string | undefined {
  try {
    const url = new URL(value);
    if (url.protocol !== 'https:' || url.username || url.password) return undefined;
    return url.href;
  } catch { return undefined; }
}

export function vercelSyncStatus(connection: VercelConnection, intervalSeconds = 120, now = Date.now()): { label: string; variant: BadgeVariant } {
  if (!connection.enabled) return { label: 'Paused', variant: 'muted' };
  if (!connection.hasToken) return { label: 'Token needed', variant: 'fail' };
  if (!connection.projectIds.length) return { label: 'Choose projects', variant: 'ember' };
  if (connection.syncing) return { label: 'Syncing', variant: 'running' };
  if (connection.syncError) return { label: 'Sync failed', variant: 'fail' };
  const last = Date.parse(connection.lastSuccessAt ?? '');
  if (!Number.isFinite(last)) return { label: 'Not synced yet', variant: 'muted' };
  if (now - last > Math.max(60, intervalSeconds) * 2_000) return { label: 'Out of date', variant: 'ember' };
  return { label: 'Up to date', variant: 'ok' };
}

export function vercelDeploymentStatus(state: string): { label: string; variant: BadgeVariant } {
  switch (state.toUpperCase()) {
    case 'READY': return { label: 'Ready', variant: 'ok' };
    case 'ERROR': return { label: 'Failed', variant: 'fail' };
    case 'BUILDING': return { label: 'Building', variant: 'running' };
    case 'QUEUED': return { label: 'Queued', variant: 'muted' };
    case 'INITIALIZING': return { label: 'Initializing', variant: 'running' };
    case 'CANCELED': return { label: 'Canceled', variant: 'muted' };
    default: return { label: state || 'Unknown', variant: 'muted' };
  }
}
