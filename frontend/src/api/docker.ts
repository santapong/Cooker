import { get, post, del } from './client';
import type { ImageInfo, ContainerInfo } from '../types/docker';

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
};
