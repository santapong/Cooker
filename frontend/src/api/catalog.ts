import { get } from './client';
import type { CatalogApp, CatalogAppDetail, CatalogList, CatalogPipeline, CatalogPipelineDetail, CatalogQuery, CatalogService } from '../types/catalog';
export function catalogQuery(query: CatalogQuery = {}): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) if (value !== undefined && value !== '') params.set(key, String(value));
  return params.size ? '?' + params.toString() : '';
}
export const catalogApi = {
  apps: (query?: CatalogQuery) => get<CatalogList<CatalogApp>>('/catalog/apps' + catalogQuery(query)),
  app: (id: string) => get<CatalogAppDetail>('/catalog/apps/' + encodeURIComponent(id)),
  services: (query?: CatalogQuery) => get<CatalogList<CatalogService>>('/catalog/services' + catalogQuery(query)),
  pipelines: (query?: CatalogQuery) => get<CatalogList<CatalogPipeline>>('/catalog/pipelines' + catalogQuery(query)),
  pipeline: (id: string) => get<CatalogPipelineDetail>('/catalog/pipelines/' + encodeURIComponent(id)),
};
