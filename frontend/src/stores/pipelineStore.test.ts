/**
 * pipelineStore — the editor's definition/canvas contract added for the
 * porthole (P2): constellation edges, dirty tracking, moveStage/updateStage,
 * addStage returning the new id, save adopting the server's version.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Pipeline } from '../types/pipeline';

const updateMock = vi.fn();
const getMock = vi.fn();
vi.mock('../api/pipelines', () => ({
  pipelineApi: {
    get: (id: string) => getMock(id),
    update: (id: string, data: Pipeline) => updateMock(id, data),
    validate: vi.fn(),
  },
}));

import { usePipelineStore } from './pipelineStore';

const BASE: Pipeline = {
  id: 'p1',
  name: 'deploy-web',
  description: '',
  stages: [
    { id: 'build', name: 'build', type: 'build', config: { dockerfile: 'Dockerfile' }, position: { x: 0, y: 0 } },
    { id: 'push', name: 'push', type: 'push', config: {}, position: { x: 200, y: 0 } },
  ],
  edges: [{ id: 'e1', source: 'build', target: 'push', condition: 'failure' }],
  variables: {},
  createdAt: '',
  updatedAt: '',
};

describe('pipelineStore (porthole contract)', () => {
  beforeEach(() => {
    updateMock.mockReset();
    getMock.mockReset();
    usePipelineStore.getState().clearDrafts();
    usePipelineStore.setState({ pipeline: null, nodes: [], edges: [], selectedNodeId: null, dirty: false });
    usePipelineStore.getState().setPipeline(structuredClone(BASE));
  });

  it('maps edges to the constellation edge type with the condition in data', () => {
    const [edge] = usePipelineStore.getState().edges;
    expect(edge.type).toBe('constellation');
    expect(edge.data).toEqual({ condition: 'failure' });
    expect(edge.style).toBeUndefined();
  });

  it('starts clean and turns dirty on any definition change', () => {
    const s = usePipelineStore.getState();
    expect(s.dirty).toBe(false);
    s.updateStageConfig('build', { context: '.' });
    expect(usePipelineStore.getState().dirty).toBe(true);
  });

  it('addStage returns the new id and creates a node of that stage type', () => {
    const id = usePipelineStore.getState().addStage('test', { x: 10, y: 20 });
    expect(id).toMatch(/^stage-/);
    const node = usePipelineStore.getState().nodes.find((n) => n.id === id);
    expect(node?.type).toBe('test');
    expect(node?.position).toEqual({ x: 10, y: 20 });
  });

  it('moveStage persists a drag into the definition without touching nodes', () => {
    const before = usePipelineStore.getState().nodes;
    usePipelineStore.getState().moveStage('build', { x: 40, y: 60 });
    const { pipeline, nodes, dirty } = usePipelineStore.getState();
    expect(pipeline?.stages.find((s) => s.id === 'build')?.position).toEqual({ x: 40, y: 60 });
    expect(nodes).toBe(before);
    expect(dirty).toBe(true);
  });

  it('moveStage is a no-op for an unchanged position', () => {
    usePipelineStore.getState().moveStage('build', { x: 0, y: 0 });
    expect(usePipelineStore.getState().dirty).toBe(false);
  });

  it('updateStage renames and the node label follows', () => {
    usePipelineStore.getState().updateStage('build', { name: 'compile' });
    const node = usePipelineStore.getState().nodes.find((n) => n.id === 'build');
    expect(node?.data.label).toBe('compile');
  });

  it('connectStages ignores a duplicate edge', () => {
    usePipelineStore.getState().connectStages('build', 'push');
    expect(usePipelineStore.getState().pipeline?.edges).toHaveLength(1);
    usePipelineStore.getState().connectStages('push', 'build');
    expect(usePipelineStore.getState().pipeline?.edges).toHaveLength(2);
  });

  it('savePipeline adopts the server copy and clears dirty', async () => {
    usePipelineStore.getState().updateStageConfig('build', { context: 'src' });
    updateMock.mockResolvedValue({ ...BASE, version: 7 });
    await usePipelineStore.getState().savePipeline();
    expect(updateMock).toHaveBeenCalledWith('p1', expect.objectContaining({ id: 'p1' }));
    expect(usePipelineStore.getState().pipeline?.version).toBe(7);
    expect(usePipelineStore.getState().dirty).toBe(false);
  });

  it('navigation back to a pipeline preserves its draft and resets selection', async () => {
    usePipelineStore.getState().setSelectedNode('build');
    usePipelineStore.getState().updateStageConfig('build', { context: 'x' });
    getMock.mockResolvedValue(structuredClone(BASE));
    await usePipelineStore.getState().loadPipeline('p1');
    expect(usePipelineStore.getState().selectedNodeId).toBeNull();
    expect(usePipelineStore.getState().dirty).toBe(true);
    expect(usePipelineStore.getState().pipeline?.stages[0].config.context).toBe('x');
    expect(getMock).not.toHaveBeenCalled();
  });

  it('retains source and registry edits when a concurrent save is rejected', async () => {
    const source = { connectionId: 'git1', repository: 'team/app', fullRef: 'refs/tags/v1.0.0' };
    const nodes = usePipelineStore.getState().nodes;
    usePipelineStore.getState().setSource(source);
    source.fullRef = 'refs/heads/unreviewed';
    expect(usePipelineStore.getState().nodes).toBe(nodes);
    usePipelineStore.getState().updateStageConfig('build', { registryConnectionIds: ['reg1'] });
    updateMock.mockRejectedValue(new Error('409: pipeline was changed'));
    await expect(usePipelineStore.getState().savePipeline()).rejects.toThrow('409');
    const state = usePipelineStore.getState();
    expect(state.dirty).toBe(true);
    expect(state.pipeline?.source?.fullRef).toBe('refs/tags/v1.0.0');
    expect(state.pipeline?.stages[0].config.registryConnectionIds).toEqual(['reg1']);
  });

  it('saves explicit source removal and adopts the new server version', async () => {
    usePipelineStore.getState().setPipeline({ ...structuredClone(BASE), version: 6, source: { connectionId: 'git1', repository: 'team/app', fullRef: 'refs/heads/main' } });
    usePipelineStore.getState().setSource(null);
    updateMock.mockImplementation(async (_id: string, input: Pipeline) => ({ ...input, version: 7 }));
    await usePipelineStore.getState().savePipeline();
    expect(updateMock).toHaveBeenCalledWith('p1', expect.objectContaining({ version: 6, source: null }));
    expect(usePipelineStore.getState().pipeline?.version).toBe(7);
    expect(usePipelineStore.getState().dirty).toBe(false);
  });

  it('keeps newer edits made while save is in flight with the accepted server version', async () => {
    let accept!: (pipeline: Pipeline) => void;
    updateMock.mockImplementation(() => new Promise<Pipeline>((resolve) => { accept = resolve; }));
    usePipelineStore.getState().setSource({ connectionId: 'git1', repository: 'team/app', fullRef: 'refs/heads/main' });
    const accepted = structuredClone(usePipelineStore.getState().pipeline!);
    const saving = usePipelineStore.getState().savePipeline();
    usePipelineStore.getState().updateStageConfig('build', { registryConnectionIds: ['reg-later'] });
    accept({ ...accepted, version: 8 });
    await saving;
    expect(usePipelineStore.getState().pipeline?.stages[0].config.registryConnectionIds).toEqual(['reg-later']);
    expect(usePipelineStore.getState().pipeline?.version).toBe(8);
    expect(usePipelineStore.getState().dirty).toBe(true);
  });

  it('does not replace a newly loaded pipeline when an earlier save completes', async () => {
    let accept!: (pipeline: Pipeline) => void;
    updateMock.mockImplementation(() => new Promise<Pipeline>((resolve) => { accept = resolve; }));
    const saving = usePipelineStore.getState().savePipeline();
    usePipelineStore.getState().setPipeline({ ...structuredClone(BASE), id: 'p2' });
    accept({ ...structuredClone(BASE), version: 8 });
    await saving;
    expect(usePipelineStore.getState().pipeline?.id).toBe('p2');
  });
});
