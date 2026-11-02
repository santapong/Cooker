import type { VercelConnection, VercelLandingArtifact } from '../types/vercel';
import type { VercelLandingAdoption } from '../types/vercelLandingAdoption';
import type { VercelLandingUpdateReview } from '../types/vercelLandingUpdateReview';
import { landingAdoptionConnection, landingAdoptionState } from './vercelLandingAdoptions';

export interface LandingUpdateReviewCandidate {
  key: string;
  adoption: VercelLandingAdoption;
  connection: VercelConnection;
}

export interface LandingUpdateReviewState {
  label: string;
  explanation: string;
  variant: 'muted' | 'ember';
}

export function landingUpdateReviewCandidates(adoptions: VercelLandingAdoption[], connections: VercelConnection[]): LandingUpdateReviewCandidate[] {
  const candidates: LandingUpdateReviewCandidate[] = [];
  for (const adoption of adoptions) {
    const connection = landingAdoptionConnection(adoption, connections);
    // Reuse the existing adoption view's complete assessment/version guard.
    if (!connection || !Number.isInteger(adoption.version) || adoption.version < 1
      || landingAdoptionState(adoption, connections).label !== 'Adoption recorded') continue;
    candidates.push({
      key: JSON.stringify([adoption.id, adoption.version, connection.id, connection.version]),
      adoption, connection,
    });
  }
  return candidates.sort((a, b) => a.adoption.projectName.localeCompare(b.adoption.projectName) || a.key.localeCompare(b.key));
}

/** Recorded addresses are display links only; never requests or health checks. */
export function landingReviewDestination(value: string): string | undefined {
  try {
    if (typeof value !== 'string' || value.length > 2048 || !/^https:\/\/[A-Za-z0-9.-]+\/?$/.test(value)) return undefined;
    const url = new URL(value);
    if (url.protocol !== 'https:' || url.username || url.password || url.port || url.search || url.hash
      || url.pathname !== '/' || !url.hostname.includes('.') || url.hostname.length > 253 || /^\d+(?:\.\d+){3}$/.test(url.hostname)) return undefined;
    if (['.localhost', '.local', '.internal', '.test', '.invalid', '.example', '.onion'].some((suffix) => url.hostname.endsWith(suffix))) return undefined;
    if (url.hostname.split('.').some((label) => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))) return undefined;
    return url.origin;
  } catch { return undefined; }
}

function receiptSupported(review: VercelLandingUpdateReview): boolean {
  const assessment = review.assessment;
  return !!review.id && !!review.adoptionId && !!review.connectionId
    && Number.isInteger(review.adoptionVersion) && review.adoptionVersion > 0
    && Number.isInteger(review.connectionVersion) && review.connectionVersion > 0
    && review.status === 'reviewed' && review.submissionAvailable === false
    && review.environment === 'production' && review.deploymentOwner === 'cooker_manual'
    && review.sourceKind === 'embedded_cooker_landing' && review.action === 'update_landing'
    && /^[a-f0-9]{64}$/.test(review.artifactSha256)
    && /^[a-f0-9]{64}$/.test(review.provenanceArtifactSha256)
    && Number.isInteger(review.artifactFileCount) && review.artifactFileCount > 0
    && Number.isInteger(review.artifactBytes) && review.artifactBytes > 0
    && /^dpl_[A-Za-z0-9_-]+$/.test(review.baselineDeploymentId)
    && Array.isArray(review.baselineAliases) && review.baselineAliases.length <= 200
    && review.baselineAliases.every((value) => landingReviewDestination(value) !== undefined)
    && Number.isFinite(Date.parse(review.baselineDeploymentObservedAt))
    && Number.isFinite(Date.parse(review.baselineAliasesObservedAt))
    && !!assessment && assessment.eligible === true && assessment.projectId === review.projectId
    && assessment.ownerId === review.accountId && assessment.productionDeploymentId === review.baselineDeploymentId
    && Number.isFinite(Date.parse(assessment.observedAt))
    && Array.isArray(assessment.reasons) && assessment.reasons.length === 0;
}

function reviewMatchesAdoption(review: VercelLandingUpdateReview, adoption: VercelLandingAdoption): boolean {
  return adoption.id === review.adoptionId && adoption.version === review.adoptionVersion
    && adoption.connectionId === review.connectionId && adoption.connectionVersion === review.connectionVersion
    && adoption.accountScope === review.accountScope && adoption.accountId === review.accountId
    && adoption.teamId === review.teamId && adoption.projectId === review.projectId
    && adoption.environment === review.environment && adoption.deploymentOwner === review.deploymentOwner
    && adoption.sourceKind === review.sourceKind
    && adoption.provenancePublicationId === review.provenancePublicationId
    && adoption.provenanceDeploymentId === review.provenanceDeploymentId;
}

export function landingUpdateReviewState(review: VercelLandingUpdateReview, input: {
  adoptions: VercelLandingAdoption[];
  connections: VercelConnection[];
  adoptionsAvailable: boolean;
  connectionsLoading: boolean;
  currentArtifact?: VercelLandingArtifact;
  artifactAvailable: boolean;
  now?: number;
}): LandingUpdateReviewState {
  const now = input.now ?? Date.now();
  const created = Date.parse(review.createdAt);
  const expires = Date.parse(review.expiresAt);
  if (!receiptSupported(review) || !Number.isFinite(now) || !Number.isFinite(created) || !Number.isFinite(expires) || expires <= created) {
    return { label: 'Needs inspection', explanation: 'This receipt has an unknown scope, observation, or time. It cannot authorize a deployment.', variant: 'ember' };
  }
  if (now >= expires) return { label: 'Expired', explanation: 'The review expiry passed. This receipt remains available as history.', variant: 'muted' };
  if (!input.adoptionsAvailable || input.connectionsLoading) {
    return { label: 'Current scope unknown', explanation: 'Current adoption or connection information is unavailable. The recorded baseline is historical.', variant: 'ember' };
  }
  const adoption = input.adoptions.find((item) => item.id === review.adoptionId);
  if (!adoption || !reviewMatchesAdoption(review, adoption)
    || !landingUpdateReviewCandidates([adoption], input.connections).length) {
    return { label: 'Stale review', explanation: 'The adoption, connection, or permitted scope changed. Prepare a new review after checking the current records.', variant: 'ember' };
  }
  if (!input.artifactAvailable || !input.currentArtifact || !/^[a-f0-9]{64}$/.test(input.currentArtifact.sha256)) {
    return { label: 'Source freshness unknown', explanation: 'The current embedded bundle is unavailable. Its match to this recorded artifact has not been checked.', variant: 'ember' };
  }
  if (review.artifactSha256 !== input.currentArtifact.sha256
    || review.artifactFileCount !== input.currentArtifact.fileCount || review.artifactBytes !== input.currentArtifact.bytes) {
    return { label: 'Stale review', explanation: 'The embedded landing bundle changed. This receipt describes the previous artifact.', variant: 'ember' };
  }
  return { label: 'Review recorded', explanation: 'The loaded local scope and source match this receipt. Production and addresses were observed at review time and may have changed. Deployment submission is unavailable.', variant: 'muted' };
}
