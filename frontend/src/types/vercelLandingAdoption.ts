import type { VercelLandingOperation } from './vercel';

// Optional provenance fields keep older publication responses readable. Their
// absence never authorizes matching by an account or project display name.
export interface VercelLandingProvenance extends VercelLandingOperation {
  accountScope?: string;
  accountId?: string;
  teamId?: string;
}

export interface VercelStaticProjectAssessment {
  projectId: string;
  ownerId: string;
  name: string;
  productionDeploymentId?: string;
  frameworkKnown: boolean;
  frameworkOther: boolean;
  buildCommandKnown: boolean;
  buildCommandEmpty: boolean;
  installCommandKnown: boolean;
  installCommandEmpty: boolean;
  outputDirectoryKnown: boolean;
  outputDirectoryDot: boolean;
  rootDirectoryKnown: boolean;
  rootDirectoryDefault: boolean;
  gitLinkKnown: boolean;
  gitLinked: boolean;
  servicesKnown: boolean;
  hasServices: boolean;
  observedAt: string;
  eligible: boolean;
  reasons: string[];
}

export interface VercelLandingAdoption {
  id: string;
  version: number;
  enabled: boolean;
  connectionId: string;
  connectionVersion: number;
  accountScope: string;
  accountId: string;
  teamId: string;
  projectId: string;
  projectName: string;
  environment: string;
  deploymentOwner: string;
  sourceKind: string;
  permittedActions: string[];
  provenancePublicationId: string;
  provenanceDeploymentId: string;
  assessment: VercelStaticProjectAssessment | null;
  createdBy: string;
  updatedBy: string;
  createdAt: string;
  updatedAt: string;
}

export interface VercelLandingAdoptionCreate {
  connectionId: string;
  connectionVersion: number;
  projectId: string;
  provenancePublicationId: string;
  confirmManualUpdates: boolean;
}

export interface VercelLandingAdoptionUpdate {
  version: number;
  connectionVersion: number;
  enabled: boolean;
  confirmManualUpdates: boolean;
}
