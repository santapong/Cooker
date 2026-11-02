import { expect, it, vi } from 'vitest';
import { get } from './client';
import { notificationDeliveriesApi, notificationDeliveryQuery } from './notificationDeliveries';

vi.mock('./client', () => ({ get: vi.fn().mockResolvedValue({ items: [], total: 0 }) }));

it('preserves exact filter identities and server pagination without adding credentials', async () => {
  const query = { state: 'needs_attention', targetId: 'target/a? &%', runId: 'run:123', limit: 25, offset: 50 };
  const parsed = new URLSearchParams(notificationDeliveryQuery(query));
  expect(parsed.get('targetId')).toBe('target/a? &%');
  expect(parsed.get('runId')).toBe('run:123');
  expect(parsed.get('offset')).toBe('50');
  expect(notificationDeliveryQuery({ state: '', targetId: '' })).toBe('');
  await notificationDeliveriesApi.list(query);
  expect(get).toHaveBeenLastCalledWith(`/admin/notification-deliveries${notificationDeliveryQuery(query)}`);
});
