import type { IntegrationInventory } from './integrations';

export interface VercelConnection {
  id: string;
  name: string;
  teamId?: string;
  identity: { id: string; name: string; scope: 'personal' | 'team' };
  enabled: boolean;
  hasToken: boolean;
  version: number;
  projectIds: string[];
  createdAt: string;
  updatedAt: string;
  lastAttemptAt?: string;
  lastSuccessAt?: string;
  nextSyncAt?: string;
  retryAt?: string;
  syncError?: string;
  syncing: boolean;
  coverage?: { accessibleProjects: number; selectedProjects: number; observedAt: string };
  integrations?: IntegrationInventory;
}

export interface VercelProject {
  id: string;
  name: string;
  framework?: string;
}

export interface VercelDeployment {
  connectionId: string;
  id: string;
  projectId: string;
  name: string;
  url: string;
  state: string;
  target: string;
  gitBranch?: string;
  gitCommitSHA?: string;
  createdAt: string;
}

export interface VercelSnapshot {
  connections: VercelConnection[];
  projects: (VercelProject & { connectionId: string })[];
  deployments: VercelDeployment[];
  syncIntervalSeconds: number;
  historyLimit: number;
}

export interface VercelConnectionUpdate {
  version: number;
  name?: string;
  enabled?: boolean;
  token?: string;
  projectIds?: string[];
}

export interface VercelLandingArtifact {
  name: string;
  fileCount: number;
  bytes: number;
  sha256: string;
  previewUrl: string;
}

export type VercelLandingStatus = 'reviewed' | 'queued' | 'creating_project' | 'submitting' | 'checking' | 'succeeded' | 'failed' | 'needs_attention';

export interface VercelLandingOperation {
  id: string;
  connectionId: string;
  connectionVersion: number;
  accountName: string;
  projectName: string;
  projectId?: string;
  artifactSha256: string;
  target: 'production';
  status: VercelLandingStatus;
  deploymentId?: string;
  url?: string;
  message?: string;
  expiresAt: string;
  createdAt: string;
  updatedAt: string;
}

export interface VercelLandingView {
  artifact: VercelLandingArtifact;
  operations: VercelLandingOperation[];
}
