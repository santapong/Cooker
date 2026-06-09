import { get, post, put, del } from './client';

// --- Templates ---

export interface Template {
  id: string;
  name: string;
  description?: string;
  category?: string;
  schema: unknown;
  iconUrl?: string;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface TemplateInput {
  name: string;
  description?: string;
  category?: string;
  schema: unknown;
  iconUrl?: string;
  enabled?: boolean;
}

export const templatesApi = {
  list: () => get<Template[]>('/templates'),
  get: (id: string) => get<Template>(`/templates/${id}`),
  create: (data: TemplateInput) => post<Template>('/admin/templates', data),
  update: (id: string, data: TemplateInput) => put<Template>(`/admin/templates/${id}`, data),
  delete: (id: string) => del<void>(`/admin/templates/${id}`),
};

// --- Schedules ---

export interface Schedule {
  id: string;
  pipelineId: string;
  name?: string;
  cronExpr: string;
  timezone: string;
  lastRunAt?: string;
  lastRunId?: string;
  nextRunAt: string;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ScheduleInput {
  pipelineId: string;
  name?: string;
  cronExpr: string;
  timezone?: string;
  enabled?: boolean;
}

export const schedulesApi = {
  list: () => get<Schedule[]>('/admin/schedules'),
  get: (id: string) => get<Schedule>(`/admin/schedules/${id}`),
  create: (data: ScheduleInput) => post<Schedule>('/admin/schedules', data),
  update: (id: string, data: ScheduleInput) => put<Schedule>(`/admin/schedules/${id}`, data),
  delete: (id: string) => del<void>(`/admin/schedules/${id}`),
};

// --- Notification targets ---

export type NotificationKind = 'slack' | 'discord' | 'email' | 'webhook';
export type NotificationEventType =
  | 'run.succeeded'
  | 'run.failed'
  | 'run.cancelled'
  | 'deploy.succeeded'
  | 'deploy.failed'
  | 'build.failed';

export interface NotificationTarget {
  id: string;
  name: string;
  kind: NotificationKind;
  config: unknown;
  eventTypes?: NotificationEventType[];
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface NotificationTargetInput {
  name: string;
  kind: NotificationKind;
  config: unknown;
  eventTypes?: NotificationEventType[];
  enabled?: boolean;
}

export const notificationTargetsApi = {
  list: () => get<NotificationTarget[]>('/admin/notification-targets'),
  get: (id: string) => get<NotificationTarget>(`/admin/notification-targets/${id}`),
  create: (data: NotificationTargetInput) =>
    post<NotificationTarget>('/admin/notification-targets', data),
  update: (id: string, data: NotificationTargetInput) =>
    put<NotificationTarget>(`/admin/notification-targets/${id}`, data),
  delete: (id: string) => del<void>(`/admin/notification-targets/${id}`),
};

// --- Audit trail ---

export interface AuditEvent {
  id: number;
  time: string;
  userSub?: string;
  userEmail?: string;
  method: string;
  path: string;
  status: number;
  latencyMs: number;
  clientIp?: string;
}

export interface AuditEventFilters {
  /** RFC3339 lower bound, inclusive. */
  from?: string;
  /** RFC3339 upper bound, inclusive. */
  to?: string;
  /** Exact OIDC subject match. */
  user?: string;
  /** Exact HTTP method match (e.g. POST). */
  method?: string;
  /** Route-path prefix match (e.g. /api/v1/apps). */
  path?: string;
  limit?: number;
  offset?: number;
}

export interface AuditEventPage {
  events: AuditEvent[];
  limit: number;
  offset: number;
}

export const auditApi = {
  list: (filters: AuditEventFilters = {}) => {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(filters)) {
      if (v !== undefined && v !== '') qs.set(k, String(v));
    }
    const suffix = qs.toString();
    return get<AuditEventPage>(`/admin/audit${suffix ? `?${suffix}` : ''}`);
  },
};
