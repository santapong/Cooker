import { describe, expect, it } from 'vitest';
import type { VercelConnection } from '../types/vercel';
import { vercelDeploymentStatus, vercelDeploymentURL, vercelSyncStatus } from './vercel';

const now = Date.parse('2026-09-30T09:00:00Z');
const connection: VercelConnection = {
  id: 'vercel', name: 'Team', identity: { id: 'team_1', name: 'Team', scope: 'team' },
  enabled: true, hasToken: true, version: 1, projectIds: ['project'], syncing: false,
  createdAt: '2026-09-30T08:00:00Z', updatedAt: '2026-09-30T08:00:00Z', lastSuccessAt: new Date(now).toISOString(),
};

describe('Vercel deployment links', () => {
  it('keeps absolute HTTPS links only', () => {
    expect(vercelDeploymentURL('https://shop.example.test/path')).toBe('https://shop.example.test/path');
  });
  it.each(['javascript:alert(1)', 'data:text/html,hello', '//example.test', '/deploy', 'http://example.test', 'https://user:password@example.test', 'https://user@example.test', 'shop.vercel.app', ''])('rejects unsafe or ambiguous link %s', (value) => {
    expect(vercelDeploymentURL(value)).toBeUndefined();
  });
});

describe('Vercel status truth', () => {
  it('distinguishes sync progress, failures, paused access and missing selection from freshness', () => {
    expect(vercelSyncStatus(connection, 120, now).label).toBe('Up to date');
    expect(vercelSyncStatus({ ...connection, lastSuccessAt: undefined }, 120, now).label).toBe('Not synced yet');
    expect(vercelSyncStatus({ ...connection, lastSuccessAt: 'invalid' }, 120, now).label).toBe('Not synced yet');
    expect(vercelSyncStatus({ ...connection, syncing: true }, 120, now).label).toBe('Syncing');
    expect(vercelSyncStatus({ ...connection, syncError: 'Access revoked' }, 120, now).label).toBe('Sync failed');
    expect(vercelSyncStatus({ ...connection, enabled: false }, 120, now).label).toBe('Paused');
    expect(vercelSyncStatus({ ...connection, hasToken: false }, 120, now).label).toBe('Token needed');
    expect(vercelSyncStatus({ ...connection, projectIds: [] }, 120, now).label).toBe('Choose projects');
  });
  it('marks two missed server intervals as out of date', () => {
    expect(vercelSyncStatus(connection, 120, now + 240_001).label).toBe('Out of date');
    expect(vercelSyncStatus(connection, 120, now + 240_000).label).toBe('Up to date');
  });
  it('does not imply that an unknown provider state succeeded', () => {
    expect(vercelDeploymentStatus('NEW_STATE')).toEqual({ label: 'NEW_STATE', variant: 'muted' });
    expect(vercelDeploymentStatus('ERROR')).toEqual({ label: 'Failed', variant: 'fail' });
    expect(vercelDeploymentStatus('READY')).toEqual({ label: 'Ready', variant: 'ok' });
  });
});
