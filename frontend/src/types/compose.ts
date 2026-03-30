export interface ComposeBuild {
  context: string;
  dockerfile: string;
}

export interface ComposeService {
  name: string;
  image: string;
  build?: ComposeBuild;
  ports: string[];
  environment: Record<string, string>;
  dependsOn: string[];
  networks: string[];
  volumes: string[];
  command: string;
  status: string;
}

export interface ComposeConnection {
  source: string;
  target: string;
  type: 'depends_on' | 'env_reference' | 'network';
  label: string;
}

export interface ComposeGraph {
  services: ComposeService[];
  connections: ComposeConnection[];
  networks: string[];
  volumes: string[];
}
