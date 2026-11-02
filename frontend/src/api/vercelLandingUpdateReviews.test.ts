import { beforeEach, describe, expect, it, vi } from 'vitest';
import { get, post } from './client';
import { vercelLandingUpdateReviewsApi } from './vercelLandingUpdateReviews';

vi.mock('./client', () => ({ get: vi.fn(), post: vi.fn() }));

beforeEach(() => vi.clearAllMocks());

describe('existing-project update review API', () => {
  it('reads history with cancellation and does not prepare anything automatically', () => {
    const controller = new AbortController();
    vercelLandingUpdateReviewsApi.list(controller.signal);
    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith('/integrations/vercel/landing/update-reviews', controller.signal);
    expect(post).not.toHaveBeenCalled();
  });

  it('encodes one existing receipt ID', () => {
    vercelLandingUpdateReviewsApi.get('vlur_/other');
    expect(get).toHaveBeenCalledWith('/integrations/vercel/landing/update-reviews/vlur_%2Fother', undefined);
  });

  it('prepares only explicit version pins through the admin review route', () => {
    const controller = new AbortController();
    const input = { adoptionId: 'adoption_exact', adoptionVersion: 2, connectionVersion: 4 };
    vercelLandingUpdateReviewsApi.create(input, controller.signal);
    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith('/admin/integrations/vercel/landing/update-reviews', input, controller.signal);
    expect(get).not.toHaveBeenCalled();
    expect(Object.keys(vercelLandingUpdateReviewsApi).sort()).toEqual(['create', 'get', 'list']);
  });

  it('does not retry an uncertain preparation response', async () => {
    vi.mocked(post).mockRejectedValueOnce(new Error('synthetic lost response'));
    await expect(vercelLandingUpdateReviewsApi.create({ adoptionId: 'adoption_exact', adoptionVersion: 2, connectionVersion: 4 })).rejects.toThrow('synthetic lost response');
    expect(post).toHaveBeenCalledTimes(1);
    expect(get).not.toHaveBeenCalled();
  });
});
