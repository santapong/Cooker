import type { VercelConnection, VercelLandingArtifact } from '../../types/vercel';
import type { VercelLandingAdoption, VercelStaticProjectAssessment } from '../../types/vercelLandingAdoption';
import type { VercelLandingUpdateReview } from '../../types/vercelLandingUpdateReview';

export const reviewCreatedAt = '2026-10-25T01:00:00Z';
export const reviewExpiresAt = '2026-10-25T01:10:00Z';
export const reviewNow = Date.parse('2026-10-25T01:05:00Z');

export function reviewConnection(overrides: Partial<VercelConnection> = {}): VercelConnection {
  return {
    id: 'connection_exact', version: 4, name: 'Visible team label',
    identity: { scope: 'team', id: 'team_exact', name: 'Team' }, teamId: 'team_exact',
    enabled: true, hasToken: true, projectIds: ['prj_exact'], syncing: false,
    createdAt: reviewCreatedAt, updatedAt: reviewCreatedAt, ...overrides,
  };
}

export function reviewAssessment(overrides: Partial<VercelStaticProjectAssessment> = {}): VercelStaticProjectAssessment {
  return {
    projectId: 'prj_exact', ownerId: 'team_exact', name: 'Cooker landing', productionDeploymentId: 'dpl_baseline',
    frameworkKnown: true, frameworkOther: true, buildCommandKnown: true, buildCommandEmpty: true,
    installCommandKnown: true, installCommandEmpty: true, outputDirectoryKnown: true, outputDirectoryDot: true,
    rootDirectoryKnown: true, rootDirectoryDefault: true, gitLinkKnown: true, gitLinked: false,
    servicesKnown: true, hasServices: false, observedAt: '2026-10-25T00:59:59Z', eligible: true, reasons: [], ...overrides,
  };
}

export function reviewAdoption(overrides: Partial<VercelLandingAdoption> = {}): VercelLandingAdoption {
  return {
    id: 'adoption_exact', version: 2, enabled: true, connectionId: 'connection_exact', connectionVersion: 4,
    accountScope: 'team', accountId: 'team_exact', teamId: 'team_exact', projectId: 'prj_exact', projectName: 'Cooker landing',
    environment: 'production', deploymentOwner: 'cooker_manual', sourceKind: 'embedded_cooker_landing', permittedActions: ['update_landing'],
    provenancePublicationId: 'publication_original', provenanceDeploymentId: 'dpl_original', assessment: reviewAssessment(),
    createdBy: 'synthetic-admin', updatedBy: 'synthetic-admin', createdAt: reviewCreatedAt, updatedAt: reviewCreatedAt, ...overrides,
  };
}

export function reviewArtifact(overrides: Partial<VercelLandingArtifact> = {}): VercelLandingArtifact {
  return { name: 'Cooker landing', fileCount: 7, bytes: 8192, sha256: 'b'.repeat(64), previewUrl: '/landing-preview/', ...overrides };
}

export function landingUpdateReview(overrides: Partial<VercelLandingUpdateReview> = {}): VercelLandingUpdateReview {
  return {
    id: 'vlur_fixture', status: 'reviewed', adoptionId: 'adoption_exact', adoptionVersion: 2,
    connectionId: 'connection_exact', connectionVersion: 4, accountScope: 'team', accountId: 'team_exact', teamId: 'team_exact',
    projectId: 'prj_exact', projectName: 'Cooker landing', environment: 'production', deploymentOwner: 'cooker_manual',
    sourceKind: 'embedded_cooker_landing', action: 'update_landing', artifactSha256: 'b'.repeat(64), artifactFileCount: 7, artifactBytes: 8192,
    baselineDeploymentId: 'dpl_baseline', baselineDeploymentObservedAt: '2026-10-25T00:59:58Z',
    baselineAliases: ['https://cooker.example.com'], baselineAliasesObservedAt: '2026-10-25T00:59:59Z', assessment: reviewAssessment(),
    provenancePublicationId: 'publication_original', provenanceDeploymentId: 'dpl_original', provenanceArtifactSha256: 'a'.repeat(64),
    createdAt: reviewCreatedAt, expiresAt: reviewExpiresAt, submissionAvailable: false, ...overrides,
  };
}
