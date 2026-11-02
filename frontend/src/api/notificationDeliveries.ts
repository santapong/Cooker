import { get } from './client';

export interface NotificationDelivery {
  id: string;
  eventId: string;
  eventType: string;
  targetId: string;
  targetVersion: number;
  kind: string;
  state: string;
  attempts: number;
  maxAttempts: number;
  nextAttemptAt: string;
  leaseUntil?: string;
  lastCode?: string;
  lastHttpStatus?: number;
  duplicatePossible: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface NotificationDeliveryPage {
  items: NotificationDelivery[];
  total: number;
  mode: string;
  pendingSourceJobs: number;
  pendingSourceRepairJobs: number;
}

export interface NotificationDeliveryQuery {
  state?: string;
  targetId?: string;
  runId?: string;
  limit?: number;
  offset?: number;
}

export function notificationDeliveryQuery(query: NotificationDeliveryQuery): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== '') params.set(key, String(value));
  }
  const encoded = params.toString();
  return encoded ? `?${encoded}` : '';
}

export const notificationDeliveriesApi = {
  list: (query: NotificationDeliveryQuery = {}) => get<NotificationDeliveryPage>(`/admin/notification-deliveries${notificationDeliveryQuery(query)}`),
};
