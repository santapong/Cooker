import { describe, expect, it } from 'vitest';
import type { VercelConnection } from '../types/vercel';
import type { VercelLandingAdoption, VercelLandingProvenance, VercelStaticProjectAssessment } from '../types/vercelLandingAdoption';
import { landingAdoptionCandidates, landingAdoptionConnection, landingAdoptionScopeSupported, landingAdoptionState, landingCandidateConfirmation, landingReauthorizationConfirmation } from './vercelLandingAdoptions';

const observedAt = '2026-10-25T01:00:00Z';

function connection(overrides: Partial<VercelConnection> = {}): VercelConnection {
  return {
    id: 'connection-1', version: 4, name: 'Visible account label',
    identity: { scope: 'team', id: 'team_exact', name: 'Team' }, teamId: 'team_exact',
    enabled: true, hasToken: true, projectIds: ['prj_exact'], syncing: false,
    createdAt: observedAt, updatedAt: observedAt, ...overrides,
  };
}

function publication(overrides: Partial<VercelLandingProvenance> = {}): VercelLandingProvenance {
  return {
    id: 'publication-1', connectionId: 'connection-1', connectionVersion: 1,
    accountScope: 'team', accountId: 'team_exact', teamId: 'team_exact',
    accountName: 'Old account label', projectName: 'Old project label', projectId: 'prj_exact',
    artifactSha256: 'a'.repeat(64), target: 'production', status: 'succeeded', deploymentId: 'dpl_exact',
    createdAt: observedAt, updatedAt: observedAt, expiresAt: observedAt, ...overrides,
  };
}

function assessment(overrides: Partial<VercelStaticProjectAssessment> = {}): VercelStaticProjectAssessment {
  return {
    projectId: 'prj_exact', ownerId: 'team_exact', name: 'Current project label', productionDeploymentId: 'dpl_current',
    frameworkKnown: true, frameworkOther: true, buildCommandKnown: true, buildCommandEmpty: true,
    installCommandKnown: true, installCommandEmpty: true, outputDirectoryKnown: true, outputDirectoryDot: true,
    rootDirectoryKnown: true, rootDirectoryDefault: true, gitLinkKnown: true, gitLinked: false,
    servicesKnown: true, hasServices: false, observedAt, eligible: true, reasons: [], ...overrides,
  };
}

function adoption(overrides: Partial<VercelLandingAdoption> = {}): VercelLandingAdoption {
  return {
    id: 'adoption-1', version: 2, enabled: true, connectionId: 'connection-1', connectionVersion: 4,
    accountScope: 'team', accountId: 'team_exact', teamId: 'team_exact',
    projectId: 'prj_exact', projectName: 'Current project label', environment: 'production',
    deploymentOwner: 'cooker_manual', sourceKind: 'embedded_cooker_landing', permittedActions: ['update_landing'],
    provenancePublicationId: 'publication-1', provenanceDeploymentId: 'dpl_exact',
    assessment: assessment(), createdBy: 'admin', updatedBy: 'admin', createdAt: observedAt, updatedAt: observedAt,
    ...overrides,
  };
}

describe('existing landing adoption candidates', () => {
  it('uses exact account, connection, project and successful deployment receipt despite renamed labels', () => {
    const candidates = landingAdoptionCandidates([publication()], [connection()], []);
    expect(candidates).toHaveLength(1);
    expect(candidates[0].publication.id).toBe('publication-1');
    expect(candidates[0].connection.version).toBe(4);
    // The original publication version is historical; a new adoption uses the
    // current connection version, then the server revalidates its identity.
    expect(candidates[0].publication.connectionVersion).toBe(1);
  });

  it.each([
    { accountId: 'team_other' }, { accountScope: 'personal' }, { teamId: 'team_other' },
    { accountId: undefined }, { accountScope: undefined }, { teamId: undefined },
    { connectionId: 'unrelated-connection' }, { projectId: 'prj_other' },
    { deploymentId: undefined }, { artifactSha256: 'not-an-artifact' },
  ])('never uses same-name or incomplete provenance as authority: %j', (changes) => {
    expect(landingAdoptionCandidates([publication(changes)], [connection()], [])).toEqual([]);
  });

  it.each(['reviewed', 'queued', 'failed', 'needs_attention'] as const)('excludes a %s publication', (status) => {
    expect(landingAdoptionCandidates([publication({ status })], [connection()], [])).toEqual([]);
  });

  it.each([{ enabled: false }, { hasToken: false }, { projectIds: [] }, { version: 0 }])('requires the current selected connection: %j', (changes) => {
    expect(landingAdoptionCandidates([publication()], [connection(changes)], [])).toEqual([]);
  });

  it('does not substitute another connection merely because it reaches the same account/project', () => {
    expect(landingAdoptionCandidates([publication()], [connection({ id: 'another-connection' })], [])).toEqual([]);
  });

  it('keeps team and personal account identity separate', () => {
    const personalConnection = connection({ teamId: undefined, identity: { scope: 'personal', id: 'user_exact', name: 'Team' } });
    const personalPublication = publication({ accountScope: 'personal', accountId: 'user_exact', teamId: '' });
    expect(landingAdoptionCandidates([personalPublication], [personalConnection], [])).toHaveLength(1);
    expect(landingAdoptionCandidates([publication()], [personalConnection], [])).toEqual([]);
    expect(landingAdoptionCandidates([publication({ ...personalPublication, teamId: 'team_exact' })], [personalConnection], [])).toEqual([]);
  });

  it('chooses one latest successful receipt per destination and preserves the inputs', () => {
    const older = publication();
    const newer = publication({ id: 'publication-2', updatedAt: '2026-10-25T02:00:00Z', deploymentId: 'dpl_newer' });
    const failed = publication({ id: 'publication-3', updatedAt: '2026-10-25T03:00:00Z', status: 'failed' });
    const operations = [failed, older, newer];
    const before = JSON.stringify(operations);
    const candidates = landingAdoptionCandidates(operations, [connection()], []);
    expect(candidates).toHaveLength(1);
    expect(candidates[0].publication.id).toBe('publication-2');
    expect(JSON.stringify(operations)).toBe(before);
  });

  it('retains a revoked destination as history instead of offering a duplicate adoption', () => {
    expect(landingAdoptionCandidates([publication()], [connection()], [adoption({ enabled: false })])).toEqual([]);
    // Overlapping historical connections still represent one destination.
    expect(landingAdoptionCandidates([publication()], [connection()], [adoption({ connectionId: 'old-connection' })])).toEqual([]);
  });
});

describe('versioned adoption state and confirmations', () => {
  it('labels local recorded scope without claiming deployment permission or application health', () => {
    expect(landingAdoptionState(adoption(), [connection()])).toEqual({
      label: 'Adoption recorded', variant: 'muted',
      explanation: 'Cooker recorded this limited manual-update scope. Project support and permission are checked again before any future deployment.',
    });
  });

  it.each([
    [], [connection({ enabled: false })], [connection({ hasToken: false })],
    [connection({ projectIds: [] })], [connection({ version: 5 })],
    [connection({ identity: { scope: 'team', id: 'team_changed', name: 'Team' }, teamId: 'team_changed' })],
  ].map((connections) => ({ connections })))('requires review for a missing, changed or disabled connection: %j', ({ connections }) => {
    expect(landingAdoptionState(adoption(), connections).label).toBe('Needs review');
  });

  it('keeps connection loading distinct from a valid recorded match', () => {
    expect(landingAdoptionState(adoption(), [connection()], true).label).toBe('Needs review');
  });

  it('retains revoked history after disconnect and permits no reauthorization through another account', () => {
    expect(landingAdoptionState(adoption({ enabled: false }), []).label).toBe('Revoked');
    expect(landingAdoptionConnection(adoption(), [connection({ identity: { scope: 'team', id: 'team_other', name: 'Team' }, teamId: 'team_other' })])).toBeUndefined();
  });

  it('lets an administrator explicitly reauthorize the same identity at a newer connection version', () => {
    expect(landingAdoptionConnection(adoption({ enabled: false }), [connection({ version: 5 })])?.version).toBe(5);
  });

  it.each([
    { environment: 'preview' }, { deploymentOwner: 'provider_git' }, { sourceKind: 'unknown' },
    { permittedActions: ['update_landing', 'delete_project'] },
  ])('keeps an unknown/future scope unavailable: %j', (changes) => {
    const record = adoption(changes);
    expect(landingAdoptionScopeSupported(record)).toBe(false);
    expect(landingAdoptionConnection(record, [connection()])).toBeUndefined();
    expect(landingAdoptionState(record, [connection()]).label).toBe('Needs review');
  });

  it.each([
    null, assessment({ eligible: false }), assessment({ projectId: 'prj_other' }),
    assessment({ ownerId: 'team_other' }), assessment({ observedAt: 'invalid' }),
    assessment({ servicesKnown: false }), assessment({ gitLinked: true }),
    assessment({ productionDeploymentId: undefined }), assessment({ reasons: ['framework_unknown'] }),
  ])('requires review when the project assessment is missing or unsupported: %j', (value) => {
    expect(landingAdoptionState(adoption({ assessment: value }), [connection()]).label).toBe('Needs review');
  });

  it('invalidates create confirmation when the connection version or deployment provenance changes', () => {
    const original = landingAdoptionCandidates([publication()], [connection()], [])[0];
    expect(landingCandidateConfirmation(original)).not.toBe(landingCandidateConfirmation({ ...original, connection: connection({ version: 5 }) }));
    expect(landingCandidateConfirmation(original)).not.toBe(landingCandidateConfirmation({ ...original, publication: publication({ deploymentId: 'dpl_changed' }) }));
  });

  it('invalidates reauthorization confirmation when either accepted version changes', () => {
    const original = landingReauthorizationConfirmation(adoption(), connection());
    expect(original).not.toBe(landingReauthorizationConfirmation(adoption({ version: 3 }), connection()));
    expect(original).not.toBe(landingReauthorizationConfirmation(adoption(), connection({ version: 5 })));
  });
});
