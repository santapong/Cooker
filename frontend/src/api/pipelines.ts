import { get, post, put, del } from './client';
import type { Pipeline, PipelineRun, EnvironmentStatus } from '../types/pipeline';

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
  cancelRun: (pipelineId: string, runId: string) =>
    post<{ status: string }>(`/pipelines/${pipelineId}/runs/${runId}/cancel`),
  getStageLogs: (pipelineId: string, runId: string, stageId: string) =>
    get<{ logs: string }>(`/pipelines/${pipelineId}/runs/${runId}/logs/${stageId}`),
  promoteRun: (pipelineId: string, runId: string, toEnvironmentId: string) =>
    post<{ status: string }>(`/pipelines/${pipelineId}/runs/${runId}/promote`, { environmentId: toEnvironmentId }),
  approvePromotion: (pipelineId: string, runId: string, environmentId: string) =>
    post<{ status: string }>(`/pipelines/${pipelineId}/runs/${runId}/approve`, { environmentId }),
  envStatus: (pipelineId: string, runId: string) =>
    get<EnvironmentStatus[]>(`/pipelines/${pipelineId}/runs/${runId}/env-status`),
};

