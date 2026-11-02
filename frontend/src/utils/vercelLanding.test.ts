import { describe, expect, it } from 'vitest';
import type { VercelLandingOperation } from '../types/vercel';
import { landingInProgress, landingReviewCurrent, landingStatus, vercelLandingPreviewURL } from './vercelLanding';

const operation: VercelLandingOperation = {
  id: 'op-1', connectionId: 'conn-1', connectionVersion: 4, accountName: 'Team', projectName: 'cooker-landing-unique',
  artifactSha256: 'a'.repeat(64), target: 'production', status: 'reviewed', expiresAt: '2026-10-01T12:00:00Z', createdAt: '2026-10-01T11:45:00Z', updatedAt: '2026-10-01T11:45:00Z',
};
const now = Date.parse('2026-10-01T11:50:00Z');

describe('landing artifact preview boundary', () => {
  it('links only the fixed public artifact route on the configured API origin', () => {
    expect(vercelLandingPreviewURL('/landing-preview/', '')).toBe('/landing-preview/');
    expect(vercelLandingPreviewURL('/landing-preview/', 'https://api.example.test')).toBe('https://api.example.test/landing-preview/');
    expect(vercelLandingPreviewURL('/landing-preview/', 'http://127.0.0.1:4644')).toBe('http://127.0.0.1:4644/landing-preview/');
  });
  it.each(['https://other.test', '//other.test', 'javascript:alert(1)', '/landing-preview/?token=secret', '/admin', '/landing-preview/../admin'])('rejects an arbitrary preview path: %s', (path) => {
    expect(vercelLandingPreviewURL(path, '')).toBeUndefined();
  });
  it.each(['https://user:password@api.test', 'javascript:alert(1)', 'https://api.test?token=x', 'https://api.test/base'])('rejects a credential-bearing or ambiguous origin: %s', (origin) => {
    expect(vercelLandingPreviewURL('/landing-preview/', origin)).toBeUndefined();
  });
});

describe('landing review and receipt truth', () => {
  it('accepts only an unexpired matching artifact and connection generation', () => {
    expect(landingReviewCurrent(operation, operation.artifactSha256, 4, now)).toBe(true);
    expect(landingReviewCurrent(operation, 'b'.repeat(64), 4, now)).toBe(false);
    expect(landingReviewCurrent(operation, operation.artifactSha256, 5, now)).toBe(false);
    expect(landingReviewCurrent(operation, operation.artifactSha256, 4, Date.parse(operation.expiresAt))).toBe(false);
    expect(landingReviewCurrent({ ...operation, status: 'queued' }, operation.artifactSha256, 4, now)).toBe(false);
    expect(landingReviewCurrent({ ...operation, expiresAt: 'invalid' }, operation.artifactSha256, 4, now)).toBe(false);
  });
  it('never labels an uncertain provider outcome as published', () => {
    expect(landingStatus('needs_attention')).toEqual({ label: 'Needs checking', variant: 'ember' });
    expect(landingStatus('checking').label).toBe('Checking Vercel');
    expect(landingInProgress({ ...operation, status: 'needs_attention' })).toBe(true);
    expect(landingInProgress({ ...operation, status: 'succeeded' })).toBe(false);
  });
});
