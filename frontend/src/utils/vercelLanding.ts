import type { VercelLandingOperation } from '../types/vercel';
import type { BadgeVariant } from '../components/ui/Badge';

/** Only the fixed, public server artifact may be opened as a local preview. */
export function vercelLandingPreviewURL(path: string, apiOrigin: string): string | undefined {
  if (path !== '/landing-preview/') return undefined;
  if (!apiOrigin) return path;
  try {
    const origin = new URL(apiOrigin);
    if (!['http:', 'https:'].includes(origin.protocol) || origin.username || origin.password || origin.search || origin.hash || origin.pathname !== '/') return undefined;
    return `${origin.origin}${path}`;
  } catch { return undefined; }
}

export function landingStatus(status: VercelLandingOperation['status']): { label: string; variant: BadgeVariant } {
  switch (status) {
    case 'reviewed': return { label: 'Ready for review', variant: 'muted' };
    case 'queued': return { label: 'Queued in Cooker', variant: 'running' };
    case 'creating_project': return { label: 'Creating project', variant: 'running' };
    case 'submitting': return { label: 'Sending to Vercel', variant: 'running' };
    case 'checking': return { label: 'Checking Vercel', variant: 'running' };
    case 'succeeded': return { label: 'Published', variant: 'ok' };
    case 'failed': return { label: 'Failed', variant: 'fail' };
    case 'needs_attention': return { label: 'Needs checking', variant: 'ember' };
    default: return { label: 'Unknown', variant: 'muted' };
  }
}

export function landingInProgress(operation: VercelLandingOperation): boolean {
  return ['queued', 'creating_project', 'submitting', 'checking', 'needs_attention'].includes(operation.status);
}

export function landingReviewCurrent(operation: VercelLandingOperation, hash: string, connectionVersion: number, now = Date.now()): boolean {
  return operation.status === 'reviewed' && operation.target === 'production' && operation.artifactSha256 === hash && operation.connectionVersion === connectionVersion && Date.parse(operation.expiresAt) > now;
}
