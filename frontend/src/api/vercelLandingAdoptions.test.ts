import { beforeEach, describe, expect, it, vi } from 'vitest';
import { get, post, put } from './client';
import { vercelLandingAdoptionsApi } from './vercelLandingAdoptions';

vi.mock('./client', () => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }));

beforeEach(() => vi.clearAllMocks());

describe('landing adoption API boundaries', () => {
  it('reads existing adoption history with the caller cancellation signal', () => {
    const controller = new AbortController();
    vercelLandingAdoptionsApi.list(controller.signal);
    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith('/integrations/vercel/landing/adoptions', controller.signal);
    expect(post).not.toHaveBeenCalled();
    expect(put).not.toHaveBeenCalled();
  });

  it('encodes a single adoption ID when reading its existing record', () => {
    vercelLandingAdoptionsApi.get('record/other');
    expect(get).toHaveBeenCalledTimes(1);
    expect(get).toHaveBeenCalledWith('/integrations/vercel/landing/adoptions/record%2Fother', undefined);
  });

  it('uses the admin adoption route and sends only explicit create scope', () => {
    const input = { connectionId: 'connection-1', connectionVersion: 4, projectId: 'prj_exact', provenancePublicationId: 'publication-1', confirmManualUpdates: true };
    const controller = new AbortController();
    vercelLandingAdoptionsApi.create(input, controller.signal);
    expect(post).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledWith('/admin/integrations/vercel/landing/adoptions', input, controller.signal);
    expect(get).not.toHaveBeenCalled();
    expect(put).not.toHaveBeenCalled();
  });

  it('versions revocation/reauthorization through the existing adoption ID without a deployment request', () => {
    const input = { version: 2, connectionVersion: 4, enabled: false, confirmManualUpdates: false };
    vercelLandingAdoptionsApi.update('adoption/1', input);
    expect(put).toHaveBeenCalledTimes(1);
    expect(put).toHaveBeenCalledWith('/admin/integrations/vercel/landing/adoptions/adoption%2F1', input, undefined);
    expect(post).not.toHaveBeenCalled();
  });
});
