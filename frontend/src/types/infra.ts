export interface Host {
  id: string;
  name: string;
  kind: 'docker' | 'kubernetes';
  reachability: 'direct' | 'tailnet';
  dockerEndpoint?: string;
  kubeconfigRef?: string;
  tailnetIp?: string;
  createdAt: string;
  updatedAt: string;
}

export interface DockerNetwork {
  id: string;
  name: string;
  driver: string;
  scope: string;
  labels?: Record<string, string>;
  hostId?: string;
}

export interface DockerVolume {
  name: string;
  driver: string;
  mountpoint: string;
  labels?: Record<string, string>;
  hostId?: string;
}

export interface RegistryConfig {
  id: string;
  name: string;
  url: string;
  username?: string;
}

export interface ClusterConfig {
  id: string;
  name: string;
  context?: string;
}
