import { get, post, put, del } from './client';
import type { Pipeline, PipelineRun, EnvironmentStatus, RunDiffReport } from '../types/pipeline';

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
  getRunDiff: (pipelineId: string, runId: string, against?: string) =>
    get<RunDiffReport>(
      `/pipelines/${pipelineId}/runs/${runId}/diff${against ? `?against=${against}` : ''}`,
    ),
  envStatus: (pipelineId: string, runId: string) =>
    get<EnvironmentStatus[]>(`/pipelines/${pipelineId}/runs/${runId}/env-status`),
};

export interface ServiceRuntimeStatus {
  runtime: string;
  ref: string;
  state: string;
  healthy: boolean;
  image?: string;
  message?: string;
}

export const runtimeApi = {
  // Live container/pod state for one deployed compose service.
  serviceStatus: (appId: string, svc: string) =>
    get<ServiceRuntimeStatus>(`/apps/${appId}/services/${svc}/runtime`),
  // WebSocket path for live runtime logs (used by useRuntimeLogs).
  logsPath: (appId: string, svc: string) => `/ws/runtime/${appId}/${svc}/logs`,
};

