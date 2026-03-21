export interface KubeWorkload {
  kind: string;
  name: string;
  namespace: string;
  replicas: number;
  ready: number;
  image: string;
  status: string;
}

export interface KubeNamespace {
  name: string;
  status: string;
  labels: Record<string, string>;
}

export interface KubePod {
  name: string;
  namespace: string;
  status: string;
  node: string;
  ip: string;
  restarts: number;
}
