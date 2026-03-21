import { get, post, del } from './client';
import type { KubeNamespace, KubeWorkload } from '../types/kubernetes';

export const kubernetesApi = {
  listNamespaces: () => get<KubeNamespace[]>('/kubernetes/namespaces'),
  listWorkloads: (namespace?: string) =>
    get<KubeWorkload[]>(`/kubernetes/workloads${namespace ? `?namespace=${namespace}` : ''}`),
  getWorkload: (ns: string, kind: string, name: string) =>
    get<KubeWorkload>(`/kubernetes/workloads/${ns}/${kind}/${name}`),
  scale: (ns: string, kind: string, name: string, replicas: number) =>
    post(`/kubernetes/workloads/${ns}/${kind}/${name}/scale`, { replicas }),
  restart: (ns: string, kind: string, name: string) =>
    post(`/kubernetes/workloads/${ns}/${kind}/${name}/restart`),
  apply: (manifest: string, namespace?: string) =>
    post('/kubernetes/apply', { manifest, namespace }),
  deleteResource: (ns: string, kind: string, name: string) =>
    del(`/kubernetes/${ns}/${kind}/${name}`),
};
