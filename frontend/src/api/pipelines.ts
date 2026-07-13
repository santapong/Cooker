import { get, post, put, del, getText, postText, pageQuery, type PageParams } from './client';
import type {
  Pipeline,
  PipelineRun,
  EnvironmentStatus,
  RunDiffReport,
  PipelineAnalytics,
  StageApproval,
} from '../types/pipeline';

export const pipelineApi = {
  list: (page?: PageParams) => get<Pipeline[]>(`/pipelines${pageQuery(page)}`),
  get: (id: string) => get<Pipeline>(`/pipelines/${id}`),
  create: (data: Partial<Pipeline>) => post<Pipeline>('/pipelines', data),
  update: (id: string, data: Pipeline) => put<Pipeline>(`/pipelines/${id}`, data),
  delete: (id: string) => del(`/pipelines/${id}`),
  validate: (id: string) => post<{ valid: boolean; errors: string[] }>(`/pipelines/${id}/validate`),
  // Pipeline-as-code (product-plan Tier 2). exportYaml returns the raw
  // YAML document for download; importYaml POSTs a YAML document and
  // returns the freshly-created pipeline (new id + timestamps).
  exportYaml: (id: string) => getText(`/pipelines/${id}/export`, 'application/yaml'),
  importYaml: (yaml: string) => postText<Pipeline>('/pipelines/import', yaml),
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
  triageStage: (pipelineId: string, runId: string, stageId: string) =>
    post<{ advisory: string; model: string }>(
      `/pipelines/${pipelineId}/runs/${runId}/stages/${stageId}/triage`,
    ),
  analytics: (pipelineId: string, runs = 30) =>
    get<PipelineAnalytics>(`/pipelines/${pipelineId}/analytics?runs=${runs}`),
  envStatus: (pipelineId: string, runId: string) =>
    get<EnvironmentStatus[]>(`/pipelines/${pipelineId}/runs/${runId}/env-status`),
  // Approval-gate stages (StageTypeApproval). The run page polls
  // stageApprovals to find paused stages, then approves/rejects them.
  stageApprovals: (pipelineId: string, runId: string) =>
    get<{ runId: string; gates: StageApproval[] }>(
      `/pipelines/${pipelineId}/runs/${runId}/stage-approvals`,
    ),
  approveStage: (pipelineId: string, runId: string, stageId: string, note?: string) =>
    post<{ status: string }>(
      `/pipelines/${pipelineId}/runs/${runId}/stages/${stageId}/approve`,
      { note: note ?? '' },
    ),
  rejectStage: (pipelineId: string, runId: string, stageId: string, note?: string) =>
    post<{ status: string }>(
      `/pipelines/${pipelineId}/runs/${runId}/stages/${stageId}/reject`,
      { note: note ?? '' },
    ),
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

