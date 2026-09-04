export interface ComposeBuild {
  context: string;
  dockerfile: string;
}

/** CPU / memory limits as parsed from `deploy.resources.limits` (or `mem_limit` / `cpus`). */
export interface ComposeResourceLimits {
  memory?: string;
  memoryBytes?: number;
  cpus?: string;
  nanoCpus?: number;
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
  labels?: Record<string, string>;
  /** Deployment group-box: `com.cooker.group` label → single network → "default". */
  group?: string;
  resources?: ComposeResourceLimits;
}

/** The fields `PUT /docker/compose/services/:name` accepts. */
export type ComposeServicePatch = Pick<ComposeService, 'image' | 'ports' | 'environment'>;

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
