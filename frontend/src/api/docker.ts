import { get, post, put, del } from './client';
import type { ImageInfo, ContainerInfo } from '../types/docker';
import type { ComposeGraph, ComposeServicePatch } from '../types/compose';
import type { DockerNetwork, DockerVolume } from '../types/infra';

export const dockerApi = {
  listImages: () => get<ImageInfo[]>('/docker/images'),
  getImage: (id: string) => get<ImageInfo>(`/docker/images/${id}`),
  buildImage: (config: { dockerfile: string; context: string; tags: string[] }) =>
    post<{ buildId: string }>('/docker/images/build', config),
  deleteImage: (id: string) => del(`/docker/images/${id}`),

  listContainers: () => get<ContainerInfo[]>('/docker/containers'),
  createContainer: (config: { image: string; name?: string }) =>
    post<{ id: string }>('/docker/containers', config),
  stopContainer: (id: string) => post(`/docker/containers/${id}/stop`),
  deleteContainer: (id: string) => del(`/docker/containers/${id}`),

  parseCompose: (composePath?: string) =>
    post<ComposeGraph>('/docker/compose/parse', composePath ? { composePath } : {}),
  updateComposeService: (name: string, patch: ComposeServicePatch) =>
    put<{ message: string; service: string }>(`/docker/compose/services/${encodeURIComponent(name)}`, patch),

  listNetworks: (hostId?: string) =>
    get<DockerNetwork[]>(`/docker/networks${hostId ? `?hostId=${encodeURIComponent(hostId)}` : ''}`),
  createNetwork: (data: Partial<DockerNetwork>) =>
    post<{ id: string }>('/docker/networks', data),
  deleteNetwork: (id: string) => del(`/docker/networks/${id}`),
  connectContainerToNetwork: (id: string, containerId: string) =>
    post(`/docker/networks/${id}/connect`, { containerId }),

  listVolumes: (hostId?: string) =>
    get<DockerVolume[]>(`/docker/volumes${hostId ? `?hostId=${encodeURIComponent(hostId)}` : ''}`),
  createVolume: (data: Partial<DockerVolume>) =>
    post<{ name: string }>('/docker/volumes', data),
  deleteVolume: (name: string) => del(`/docker/volumes/${encodeURIComponent(name)}`),
};

