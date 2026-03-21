import { get, post, put, del } from './client';
import type { Pipeline, PipelineRun } from '../types/pipeline';

export const pipelineApi = {
  list: () => get<Pipeline[]>('/pipelines'),
  get: (id: string) => get<Pipeline>(`/pipelines/${id}`),
  create: (data: Partial<Pipeline>) => post<Pipeline>('/pipelines', data),
  update: (id: string, data: Pipeline) => put<Pipeline>(`/pipelines/${id}`, data),
  delete: (id: string) => del(`/pipelines/${id}`),
  validate: (id: string) => post<{ valid: boolean; errors: string[] }>(`/pipelines/${id}/validate`),
  run: (id: string) => post<PipelineRun>(`/pipelines/${id}/run`),
  listRuns: (id: string) => get<PipelineRun[]>(`/pipelines/${id}/runs`),
  getRun: (pipelineId: string, runId: string) => get<PipelineRun>(`/pipelines/${pipelineId}/runs/${runId}`),
};
