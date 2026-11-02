import { describe, expect, it } from 'vitest';
import { landingUpdateReview, reviewAdoption, reviewArtifact, reviewAssessment, reviewConnection, reviewExpiresAt, reviewNow } from '../test/fixtures/vercelLandingUpdateReviews';
import { landingReviewDestination, landingUpdateReviewCandidates, landingUpdateReviewState } from './vercelLandingUpdateReviews';

function input(overrides: Partial<Parameters<typeof landingUpdateReviewState>[1]> = {}): Parameters<typeof landingUpdateReviewState>[1] {
  return { adoptions: [reviewAdoption()], connections: [reviewConnection()], adoptionsAvailable: true, connectionsLoading: false, currentArtifact: reviewArtifact(), artifactAvailable: true, now: reviewNow, ...overrides };
}

describe('version-bound existing-project review selection', () => {
  it('keeps the exact adopted identity and accepted versions without matching names', () => {
    const rows = landingUpdateReviewCandidates([reviewAdoption({ projectName: 'Renamed landing' })], [reviewConnection({ name: 'Renamed connection' })]);
    expect(rows).toHaveLength(1);
    expect(rows[0].adoption.projectId).toBe('prj_exact');
    expect(rows[0].connection.version).toBe(4);
  });

  it.each([
    reviewAdoption({ enabled: false }), reviewAdoption({ connectionVersion: 3 }),
    reviewAdoption({ permittedActions: ['delete_project'] }), reviewAdoption({ version: 0 }),
    reviewAdoption({ assessment: reviewAssessment({ eligible: false }) }),
  ])('does not offer a revoked, stale, unsupported or unassessed adoption: %j', (adoption) => {
    expect(landingUpdateReviewCandidates([adoption], [reviewConnection()])).toEqual([]);
  });

  it.each([
    [], [reviewConnection({ enabled: false })], [reviewConnection({ hasToken: false })],
    [reviewConnection({ projectIds: [] })], [reviewConnection({ version: 5 })],
    [reviewConnection({ id: 'different_connection' })],
    [reviewConnection({ identity: { scope: 'team', id: 'team_other', name: 'Team' }, teamId: 'team_other' })],
  ].map((connections) => ({ connections })))('requires the exact current selected connection: %j', ({ connections }) => {
    expect(landingUpdateReviewCandidates([reviewAdoption()], connections)).toEqual([]);
  });

  it('invalidates selection when either version changes and leaves its inputs intact', () => {
    const adoptions = [reviewAdoption()];
    const connections = [reviewConnection()];
    const before = JSON.stringify({ adoptions, connections });
    const original = landingUpdateReviewCandidates(adoptions, connections)[0];
    const changedAdoption = landingUpdateReviewCandidates([reviewAdoption({ version: 3 })], connections)[0];
    const changedConnection = landingUpdateReviewCandidates([reviewAdoption({ connectionVersion: 5 })], [reviewConnection({ version: 5 })])[0];
    expect(original.key).not.toBe(changedAdoption.key);
    expect(original.key).not.toBe(changedConnection.key);
    expect(JSON.stringify({ adoptions, connections })).toBe(before);
  });
});

describe('inspection-only review truth', () => {
  it('keeps reviewed artifact, original publication and observed production distinct', () => {
    const review = landingUpdateReview();
    expect(review.artifactSha256).not.toBe(review.provenanceArtifactSha256);
    expect(review.baselineDeploymentId).not.toBe(review.provenanceDeploymentId);
    const state = landingUpdateReviewState(review, input());
    expect(state.label).toBe('Review recorded');
    expect(state.variant).toBe('muted');
    expect(state.explanation).toContain('may have changed');
    expect(state.explanation).toContain('Deployment submission is unavailable');
  });

  it('expires at the exact server timestamp without enabling a deployment', () => {
    expect(landingUpdateReviewState(landingUpdateReview(), input({ now: Date.parse(reviewExpiresAt) })).label).toBe('Expired');
    expect(landingUpdateReviewState(landingUpdateReview(), input({ now: Date.parse(reviewExpiresAt) + 1 })).label).toBe('Expired');
  });

  it.each([
    { status: 'submitted' }, { submissionAvailable: true }, { artifactSha256: 'invalid' },
    { baselineDeploymentId: 'not_a_deployment' }, { assessment: null },
    { assessment: reviewAssessment({ productionDeploymentId: 'dpl_unrelated' }) },
    { baselineAliasesObservedAt: 'invalid' }, { expiresAt: 'invalid' },
    { action: 'promote' }, { environment: 'preview' },
  ])('keeps unknown receipt capabilities, observations or scope unavailable: %j', (changes) => {
    expect(landingUpdateReviewState(landingUpdateReview(changes), input()).label).toBe('Needs inspection');
  });

  it.each([{ adoptionsAvailable: false }, { connectionsLoading: true }])('does not call an unchecked current scope fresh: %j', (changes) => {
    expect(landingUpdateReviewState(landingUpdateReview(), input(changes)).label).toBe('Current scope unknown');
  });

  it.each([
    { adoptions: [] }, { adoptions: [reviewAdoption({ version: 3 })] },
    { adoptions: [reviewAdoption({ enabled: false })] }, { connections: [] },
    { connections: [reviewConnection({ version: 5 })] },
    { adoptions: [reviewAdoption({ provenanceDeploymentId: 'dpl_other' })] },
  ])('preserves history while marking changed adoption/connection/provenance stale: %j', (changes) => {
    expect(landingUpdateReviewState(landingUpdateReview(), input(changes)).label).toBe('Stale review');
  });

  it.each([{ artifactAvailable: false }, { currentArtifact: undefined }])('keeps current source unknown when metadata is unavailable: %j', (changes) => {
    expect(landingUpdateReviewState(landingUpdateReview(), input(changes)).label).toBe('Source freshness unknown');
  });

  it('marks a changed current bundle stale without replacing the review artifact', () => {
    const review = landingUpdateReview();
    expect(landingUpdateReviewState(review, input({ currentArtifact: reviewArtifact({ sha256: 'c'.repeat(64) }) })).label).toBe('Stale review');
    expect(review.artifactSha256).toBe('b'.repeat(64));
  });

  it('accepts an explicitly observed empty alias set without inventing an address', () => {
    expect(landingUpdateReviewState(landingUpdateReview({ baselineAliases: [] }), input()).label).toBe('Review recorded');
  });
});

describe('recorded review destination links', () => {
  it('uses an HTTPS origin only, without fetching or asserting access', () => {
    expect(landingReviewDestination('https://cooker.example.com')).toBe('https://cooker.example.com');
    expect(landingReviewDestination('https://cooker.example.com/')).toBe('https://cooker.example.com');
  });

  it.each([
    'http://cooker.example.com', 'javascript:alert(1)', '//cooker.example.com',
    'https://user:secret@cooker.example.com', 'https://@cooker.example.com',
    'https://cooker.example.com?token=secret', 'https://cooker.example.com?',
    'https://cooker.example.com#secret', 'https://cooker.example.com/path',
    'https://cooker.example.com:443', 'https://127.0.0.1', 'https://localhost',
    'https://service.internal', 'https://escaped%2eexample.com',
  ])('does not make an unsafe or non-origin address executable: %s', (value) => {
    expect(landingReviewDestination(value)).toBeUndefined();
  });
});
