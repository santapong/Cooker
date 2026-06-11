import { get, post, del } from './client';
import type { APIToken } from '../types/token';

export type { APIToken };

export const tokensApi = {
  list: (all?: boolean) =>
    get<{ tokens: APIToken[] }>(all ? '/tokens?all=true' : '/tokens').then((r) => r.tokens),

  create: (data: { name: string; role: string; expiresAt?: string }) =>
    post<{ token: string; apiToken: APIToken }>('/tokens', data),

  remove: (id: string) => del<{ deleted: string }>(`/tokens/${id}`),
};
