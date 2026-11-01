import type { StageType } from '../../types/pipeline';

export type StageKind = StageType | 'gitops-commit' | 'service' | 'external';

export const STAGE_LABELS: Record<StageKind, string> = {
  build: 'Build', test: 'Test', push: 'Push', deploy: 'Deploy',
  approval: 'Approval', custom: 'Custom', 'gitops-commit': 'GitOps', service: 'Service',
  external: 'External database',
};
