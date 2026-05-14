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
