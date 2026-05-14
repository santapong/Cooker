export type HostKind = 'docker' | 'kubernetes' | 'ssh-docker';

export interface Host {
  id: string;
  name: string;
  kind: HostKind;
  reachability: 'direct' | 'tailnet';
  dockerEndpoint?: string;
  kubeconfigRef?: string;
  tailnetIp?: string;
  // SSH fields (kind=ssh-docker). sshPrivateKeyPem is write-only;
  // the backend never echoes it back, instead surfacing
  // hasSSHPrivateKey so the UI can render "key on file" state.
  sshEndpoint?: string;
  sshUser?: string;
  sshPort?: number;
  sshKnownHostKey?: string;
  sshStrictHostKey?: boolean;
  hasSSHPrivateKey?: boolean;
  createdAt: string;
  updatedAt: string;
}

// HostCreatePayload is the wire shape POST/PUT /hosts accepts. Adds
// the write-only sshPrivateKeyPem field.
export interface HostCreatePayload extends Partial<Host> {
  sshPrivateKeyPem?: string;
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
