import { beforeEach, expect, it, vi } from 'vitest';
import type { Pipeline } from '../types/pipeline';
const { get, update } = vi.hoisted(() => ({ get: vi.fn(), update: vi.fn() }));
vi.mock('../api/pipelines', () => ({ pipelineApi: { get, update } }));
import { usePipelineStore } from './pipelineStore';
const base = (id: string): Pipeline => ({ id, version: 1, name: id, description: '', stages: [{ id: 'step', name: 'Step', type: 'test', position: { x: 0, y: 0 }, config: {} }], edges: [], variables: {}, createdAt: '', updatedAt: '' });
beforeEach(() => { usePipelineStore.getState().clearDrafts(); get.mockReset(); update.mockReset(); get.mockImplementation(async (id) => base(id)); });
it('navigation preserves different pipelines and explicit key names without storage', async () => {
  const store = usePipelineStore.getState(); await store.loadPipeline('one'); store.updateStage('step', { environmentId: 'env-one' }); store.updateStageConfig('step', { secretRefs: ['PASSWORD'], registryConnectionIds: ['registry-one'] });
  await store.loadPipeline('two'); store.updateStageConfig('step', { secretRefs: ['DATABASE_NAME'] }); await store.loadPipeline('one');
  expect(usePipelineStore.getState().pipeline?.stages[0]).toMatchObject({ environmentId: 'env-one', config: { secretRefs: ['PASSWORD'], registryConnectionIds: ['registry-one'] } });
  expect(usePipelineStore.getState().dirty).toBe(true); expect(get).toHaveBeenCalledTimes(2);
  store.clearDrafts(); await store.loadPipeline('one'); expect(usePipelineStore.getState().pipeline?.stages[0].config.secretRefs).toBeUndefined();
});
it('an old load cannot overwrite a newer navigation or signed-out state', async () => {
  let finish!: (pipeline: Pipeline) => void; get.mockImplementationOnce(() => new Promise<Pipeline>((resolve) => { finish = resolve; }));
  const store = usePipelineStore.getState(); const old = store.loadPipeline('old'); await store.loadPipeline('new'); finish(base('old')); await old; expect(usePipelineStore.getState().pipeline?.id).toBe('new');
  get.mockImplementationOnce(() => new Promise<Pipeline>((resolve) => { finish = resolve; })); const pending = store.loadPipeline('later'); store.clearDrafts(); finish(base('later')); await pending; expect(usePipelineStore.getState().pipeline).toBeNull();
});
it('accepted save after navigation clears only the saved draft, preserving newer edits', async () => {
  let finish!: (pipeline: Pipeline) => void; update.mockImplementationOnce(() => new Promise<Pipeline>((resolve) => { finish = resolve; }));
  const store = usePipelineStore.getState(); await store.loadPipeline('one'); store.updateStageConfig('step', { secretRefs: ['PASSWORD'] }); const accepted = usePipelineStore.getState().pipeline!; const saving = store.savePipeline();
  store.updateStageConfig('step', { secretRefs: ['PASSWORD', 'DATABASE_NAME'] }); await store.loadPipeline('two'); finish({ ...accepted, version: 2 }); await saving; await store.loadPipeline('one');
  expect(usePipelineStore.getState().pipeline).toMatchObject({ version: 2, stages: [{ config: { secretRefs: ['PASSWORD', 'DATABASE_NAME'] } }] }); expect(usePipelineStore.getState().dirty).toBe(true);
});
it('old-session accepted save cannot change a freshly loaded same-ID definition', async () => {
  let finish!: (pipeline: Pipeline) => void; update.mockImplementationOnce(() => new Promise<Pipeline>((resolve) => { finish = resolve; }));
  const store = usePipelineStore.getState(); await store.loadPipeline('one'); store.updateStageConfig('step', { secretRefs: ['PASSWORD'] }); const previous = usePipelineStore.getState().pipeline!; const saving = store.savePipeline(); store.clearDrafts(); await store.loadPipeline('one'); finish({ ...previous, version: 99 }); await saving;
  expect(usePipelineStore.getState().pipeline).toMatchObject({ version: 1, stages: [{ config: {} }] }); expect(usePipelineStore.getState().dirty).toBe(false);
});
it('failed discard refresh retains a dirty definition and all explicit bindings', async () => {
  const store = usePipelineStore.getState(); await store.loadPipeline('one'); store.updateStageConfig('step', { secretRefs: ['PASSWORD'] }); const draft = usePipelineStore.getState().pipeline; get.mockRejectedValueOnce(new Error('unavailable'));
  await expect(store.discardDraft('one')).rejects.toThrow('unavailable'); expect(usePipelineStore.getState().pipeline).toBe(draft); expect(usePipelineStore.getState().dirty).toBe(true);
});
