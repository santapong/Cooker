import type { VercelStaticProjectAssessment } from './vercelLandingAdoption';

// Server-issued inspection receipt. There is deliberately no submission DTO.
export interface VercelLandingUpdateReview {
  id: string;
  status: string;
  adoptionId: string;
  adoptionVersion: number;
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
  action: string;
  artifactSha256: string;
  artifactFileCount: number;
  artifactBytes: number;
  baselineDeploymentId: string;
  baselineDeploymentObservedAt: string;
  baselineAliases: string[];
  baselineAliasesObservedAt: string;
  assessment: VercelStaticProjectAssessment | null;
  provenancePublicationId: string;
  provenanceDeploymentId: string;
  provenanceArtifactSha256: string;
  createdAt: string;
  expiresAt: string;
  submissionAvailable: boolean;
}

export interface VercelLandingUpdateReviewRequest {
  adoptionId: string;
  adoptionVersion: number;
  connectionVersion: number;
}
