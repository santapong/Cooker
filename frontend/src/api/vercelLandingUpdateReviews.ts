import { get, post } from './client';
import type { VercelLandingUpdateReview, VercelLandingUpdateReviewRequest } from '../types/vercelLandingUpdateReview';

const path = '/integrations/vercel/landing/update-reviews';

export const vercelLandingUpdateReviewsApi = {
  list: (signal?: AbortSignal) => get<{ reviews: VercelLandingUpdateReview[] | null }>(path, signal),
  get: (id: string, signal?: AbortSignal) => get<{ review: VercelLandingUpdateReview }>(`${path}/${encodeURIComponent(id)}`, signal),
  create: (input: VercelLandingUpdateReviewRequest, signal?: AbortSignal) => post<{ review: VercelLandingUpdateReview }>(`/admin${path}`, input, signal),
};
