import { get, post } from './client';

export const registryApi = {
  listRepositories: () => get<{ repositories: string[] }>('/registry/repositories'),
  listTags: (name: string) => get<{ name: string; tags: string[] }>(`/registry/${name}/tags`),
  getManifest: (name: string, ref: string) => get(`/registry/${name}/manifests/${ref}`),
  push: (image: string, registry: string, tag?: string) =>
    post('/registry/push', { image, registry, tag }),
  pull: (image: string, registry?: string) =>
    post('/registry/pull', { image, registry }),
  getReferrers: (name: string, digest: string) =>
    get(`/registry/${name}/referrers/${digest}`),
};
