import { del, get, post, put } from './client';
import type { VercelConnection, VercelConnectionUpdate, VercelLandingOperation, VercelLandingView, VercelProject, VercelSnapshot } from '../types/vercel';

const connections = '/admin/integrations/vercel/connections';
const connectionPath = (id: string) => `${connections}/${encodeURIComponent(id)}`;
const landing = '/integrations/vercel/landing';

export const vercelApi = {
  snapshot: () => get<VercelSnapshot>('/integrations/vercel'),
  connections: () => get<{ connections: VercelConnection[] }>(connections),
  create: (input: { name: string; token: string; teamId?: string }) => post<{ connection: VercelConnection }>(connections, input),
  update: (id: string, input: VercelConnectionUpdate) => put<{ connection: VercelConnection }>(connectionPath(id), input),
  disconnect: (id: string, version: number) => del<{ disconnected: boolean }>(`${connectionPath(id)}?version=${version}`),
  projects: (id: string) => get<{ projects: VercelProject[] }>(`${connectionPath(id)}/projects`),
  sync: (id: string) => post<{ connection: VercelConnection }>(`${connectionPath(id)}/sync`),
  landing: () => get<VercelLandingView>(landing),
  reviewLanding: (connectionId: string, version: number) => post<{ operation: VercelLandingOperation }>(`/admin${landing}/review`, { connectionId, version }),
  deployLanding: (id: string) => post<{ operation: VercelLandingOperation }>(`/admin${landing}/operations/${encodeURIComponent(id)}/deploy`, {}),
  landingOperation: (id: string) => get<{ operation: VercelLandingOperation }>(`${landing}/operations/${encodeURIComponent(id)}`),
  checkLanding: (id: string) => post<{ operation: VercelLandingOperation }>(`/admin${landing}/operations/${encodeURIComponent(id)}/check`, {}),
};
