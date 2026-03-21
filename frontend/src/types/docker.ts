export interface ContainerInfo {
  id: string;
  name: string;
  image: string;
  status: string;
  state: 'running' | 'exited' | 'paused';
  ports: PortBinding[];
  created: string;
  labels: Record<string, string>;
}

export interface PortBinding {
  containerPort: number;
  hostPort: number;
  protocol: string;
}

export interface ImageInfo {
  id: string;
  repoTags: string[];
  repoDigests: string[];
  size: number;
  created: string;
  layers: number;
}
