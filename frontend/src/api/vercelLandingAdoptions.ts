import { get, post, put } from './client';
import type { VercelLandingAdoption, VercelLandingAdoptionCreate, VercelLandingAdoptionUpdate } from '../types/vercelLandingAdoption';

const path = '/integrations/vercel/landing/adoptions';

export const vercelLandingAdoptionsApi = {
  list: (signal?: AbortSignal) => get<{ adoptions: VercelLandingAdoption[] | null }>(path, signal),
  get: (id: string, signal?: AbortSignal) => get<{ adoption: VercelLandingAdoption }>(`${path}/${encodeURIComponent(id)}`, signal),
  create: (input: VercelLandingAdoptionCreate, signal?: AbortSignal) => post<{ adoption: VercelLandingAdoption }>(`/admin${path}`, input, signal),
  update: (id: string, input: VercelLandingAdoptionUpdate, signal?: AbortSignal) => put<{ adoption: VercelLandingAdoption }>(`/admin${path}/${encodeURIComponent(id)}`, input, signal),
};
