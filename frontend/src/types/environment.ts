export interface Environment {
  id: string;
  name: string;
  order: number;
  target: EnvironmentTarget;
  promotion: PromotionPolicy;
  variables: Record<string, string>;
  createdAt: string;
}

export interface EnvironmentTarget {
  type: 'cluster' | 'namespace';
  clusterId: string;
  namespace: string;
  kubeContext: string;
}

export interface PromotionPolicy {
  strategy: 'auto' | 'manual';
  requiredApprovers?: number;
  autoPromoteOn?: string[];
}
