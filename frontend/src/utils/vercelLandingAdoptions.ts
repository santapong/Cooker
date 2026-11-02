import type { VercelConnection } from '../types/vercel';
import type { VercelLandingAdoption, VercelLandingProvenance } from '../types/vercelLandingAdoption';

export interface LandingAdoptionCandidate {
  key: string;
  connection: VercelConnection;
  publication: VercelLandingProvenance;
}

export function landingCandidateConfirmation(candidate: LandingAdoptionCandidate): string {
  return JSON.stringify([candidate.key, candidate.connection.version, candidate.connection.identity.scope, candidate.connection.identity.id, candidate.connection.teamId ?? '', candidate.publication.projectId, candidate.publication.deploymentId, candidate.publication.artifactSha256]);
}

export function landingReauthorizationConfirmation(adoption: VercelLandingAdoption, connection: VercelConnection): string {
  return JSON.stringify([adoption.id, adoption.version, connection.id, connection.version, connection.identity.scope, connection.identity.id, connection.teamId ?? '', adoption.projectId]);
}

function sameAccount(connection: VercelConnection, identity: { accountScope?: string; accountId?: string; teamId?: string }): boolean {
  const scope = connection.identity?.scope;
  const id = connection.identity?.id;
  if (!id || (scope !== 'personal' && scope !== 'team') || identity.accountScope !== scope || identity.accountId !== id) return false;
  return scope === 'personal'
    ? !connection.teamId && identity.teamId === ''
    : connection.teamId === id && identity.teamId === id;
}

function usableConnection(connection: VercelConnection | undefined, projectId: string): connection is VercelConnection {
  return !!connection && connection.enabled === true && connection.hasToken === true
    && Number.isInteger(connection.version) && connection.version > 0
    && Array.isArray(connection.projectIds) && connection.projectIds.includes(projectId);
}

function destination(identity: { accountScope?: string; accountId?: string; teamId?: string; projectId?: string; environment?: string }): string {
  return JSON.stringify([identity.accountScope, identity.accountId, identity.teamId, identity.projectId, identity.environment ?? 'production']);
}

export function landingAdoptionCandidates(
  operations: VercelLandingProvenance[], connections: VercelConnection[], adoptions: VercelLandingAdoption[],
): LandingAdoptionCandidate[] {
  const existing = new Set(adoptions.map(destination));
  const candidates = new Map<string, LandingAdoptionCandidate>();
  const byConnection = new Map(connections.map((connection) => [connection.id, connection]));
  for (const publication of operations) {
    if (publication.status !== 'succeeded' || publication.target !== 'production'
      || !/^prj_[A-Za-z0-9_-]+$/.test(publication.projectId ?? '')
      || !/^dpl_[A-Za-z0-9_-]+$/.test(publication.deploymentId ?? '')
      || !/^[a-f0-9]{64}$/.test(publication.artifactSha256)
      || !publication.id) continue;
    const connection = byConnection.get(publication.connectionId);
    if (!usableConnection(connection, publication.projectId!) || !sameAccount(connection, publication)) continue;
    const identity = destination(publication);
    if (existing.has(identity)) continue;
    const previous = candidates.get(identity);
    const time = Date.parse(publication.updatedAt);
    const previousTime = previous ? Date.parse(previous.publication.updatedAt) : NaN;
    if (!previous || (Number.isFinite(time) && (!Number.isFinite(previousTime) || time > previousTime))
      || (time === previousTime && publication.id < previous.publication.id)) {
      candidates.set(identity, { key: `${connection.id}:${publication.id}`, connection, publication });
    }
  }
  return [...candidates.values()].sort((a, b) => a.publication.projectName.localeCompare(b.publication.projectName) || a.key.localeCompare(b.key));
}

export function landingAdoptionScopeSupported(adoption: VercelLandingAdoption): boolean {
  return adoption.environment === 'production' && adoption.deploymentOwner === 'cooker_manual'
    && adoption.sourceKind === 'embedded_cooker_landing' && Array.isArray(adoption.permittedActions)
    && adoption.permittedActions.length === 1 && adoption.permittedActions[0] === 'update_landing';
}

export function landingAdoptionConnection(adoption: VercelLandingAdoption, connections: VercelConnection[]): VercelConnection | undefined {
  const connection = connections.find((item) => item.id === adoption.connectionId);
  return landingAdoptionScopeSupported(adoption) && usableConnection(connection, adoption.projectId) && sameAccount(connection, adoption)
    ? connection : undefined;
}

export function landingAdoptionState(adoption: VercelLandingAdoption, connections: VercelConnection[], connectionsLoading = false): { label: string; explanation: string; variant: 'muted' | 'ember' } {
  if (!adoption.enabled) return { label: 'Revoked', explanation: 'Local authority is disabled. The project and publication history are retained.', variant: 'muted' };
  if (connectionsLoading) return { label: 'Needs review', explanation: 'Current connection information is loading.', variant: 'ember' };
  if (!landingAdoptionScopeSupported(adoption)) return { label: 'Needs review', explanation: 'This record has an unsupported scope. No action is available.', variant: 'ember' };
  const connection = landingAdoptionConnection(adoption, connections);
  if (!connection) return { label: 'Needs review', explanation: 'The connection is unavailable, disabled, changed account, or no longer selects this exact project.', variant: 'ember' };
  if (connection.version !== adoption.connectionVersion) return { label: 'Needs review', explanation: 'The connection version changed. Reauthorize after reviewing the current connection.', variant: 'ember' };
  const assessment = adoption.assessment;
  if (!assessment || !assessment.eligible || assessment.projectId !== adoption.projectId
    || assessment.ownerId !== adoption.accountId || !assessment.observedAt
    || !Number.isFinite(Date.parse(assessment.observedAt)) || !Array.isArray(assessment.reasons) || assessment.reasons.length
    || assessment.frameworkKnown !== true || assessment.frameworkOther !== true
    || assessment.buildCommandKnown !== true || assessment.buildCommandEmpty !== true
    || assessment.installCommandKnown !== true || assessment.installCommandEmpty !== true
    || assessment.outputDirectoryKnown !== true || assessment.outputDirectoryDot !== true
    || assessment.rootDirectoryKnown !== true || assessment.rootDirectoryDefault !== true
    || assessment.gitLinkKnown !== true || assessment.gitLinked !== false
    || assessment.servicesKnown !== true || assessment.hasServices !== false
    || !/^dpl_[A-Za-z0-9_-]+$/.test(assessment.productionDeploymentId ?? '')) {
    return { label: 'Needs review', explanation: 'A supported project assessment is unavailable. Recheck through an administrator review.', variant: 'ember' };
  }
  return { label: 'Adoption recorded', explanation: 'Cooker recorded this limited manual-update scope. Project support and permission are checked again before any future deployment.', variant: 'muted' };
}
