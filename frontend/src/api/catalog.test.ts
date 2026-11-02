import { expect, it, vi } from 'vitest';
vi.mock('./client', () => ({ get: vi.fn() }));
import { catalogApi, catalogQuery } from './catalog';
import { get } from './client';
it('encodes filters and opaque ids while preserving zero offset', () => {
  expect(catalogQuery({ q: 'n8n & workers', management: 'vercel', offset: 0, provider: '' })).toBe('?q=n8n+%26+workers&management=vercel&offset=0');
  catalogApi.app('id/with?parts'); expect(get).toHaveBeenLastCalledWith('/catalog/apps/id%2Fwith%3Fparts');
  catalogApi.services({ environment: 'production_first' }); expect(get).toHaveBeenLastCalledWith('/catalog/services?environment=production_first');
});
