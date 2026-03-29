import { describe, it, expect } from 'vitest';
import { validateDAG } from './dagValidation';
import type { Stage, PipelineEdge } from '../types/pipeline';

function makeStage(id: string): Stage {
  return {
    id,
    name: id,
    type: 'build',
    config: {},
    position: { x: 0, y: 0 },
  };
}

describe('validateDAG', () => {
  it('returns no errors for a valid linear pipeline', () => {
    const stages = [makeStage('build'), makeStage('test'), makeStage('deploy')];
    const edges: PipelineEdge[] = [
      { id: 'e1', source: 'build', target: 'test' },
      { id: 'e2', source: 'test', target: 'deploy' },
    ];

    const errors = validateDAG(stages, edges);
    expect(errors).toEqual([]);
  });

  it('returns no errors for parallel stages', () => {
    const stages = [makeStage('a'), makeStage('b'), makeStage('c')];
    const edges: PipelineEdge[] = [
      { id: 'e1', source: 'a', target: 'c' },
      { id: 'e2', source: 'b', target: 'c' },
    ];

    const errors = validateDAG(stages, edges);
    expect(errors).toEqual([]);
  });

  it('returns no errors for empty pipeline', () => {
    const errors = validateDAG([], []);
    expect(errors).toEqual([]);
  });

  it('returns no errors for single stage with no edges', () => {
    const errors = validateDAG([makeStage('build')], []);
    expect(errors).toEqual([]);
  });

  it('detects duplicate stage IDs', () => {
    const stages = [makeStage('build'), makeStage('build')];
    const errors = validateDAG(stages, []);
    expect(errors).toContain('Duplicate stage IDs detected');
  });

  it('detects unknown edge source', () => {
    const stages = [makeStage('build')];
    const edges: PipelineEdge[] = [
      { id: 'e1', source: 'nonexistent', target: 'build' },
    ];

    const errors = validateDAG(stages, edges);
    expect(errors).toContain('Edge references unknown source: nonexistent');
  });

  it('detects unknown edge target', () => {
    const stages = [makeStage('build')];
    const edges: PipelineEdge[] = [
      { id: 'e1', source: 'build', target: 'nonexistent' },
    ];

    const errors = validateDAG(stages, edges);
    expect(errors).toContain('Edge references unknown target: nonexistent');
  });

  it('detects cycles', () => {
    const stages = [makeStage('a'), makeStage('b'), makeStage('c')];
    const edges: PipelineEdge[] = [
      { id: 'e1', source: 'a', target: 'b' },
      { id: 'e2', source: 'b', target: 'c' },
      { id: 'e3', source: 'c', target: 'a' },
    ];

    const errors = validateDAG(stages, edges);
    expect(errors).toContain('Pipeline contains a cycle');
  });

  it('detects self-loop as a cycle', () => {
    const stages = [makeStage('a')];
    const edges: PipelineEdge[] = [
      { id: 'e1', source: 'a', target: 'a' },
    ];

    const errors = validateDAG(stages, edges);
    expect(errors).toContain('Pipeline contains a cycle');
  });

  it('validates diamond DAG structure', () => {
    const stages = [
      makeStage('build'),
      makeStage('lint'),
      makeStage('test'),
      makeStage('deploy'),
    ];
    const edges: PipelineEdge[] = [
      { id: 'e1', source: 'build', target: 'lint' },
      { id: 'e2', source: 'build', target: 'test' },
      { id: 'e3', source: 'lint', target: 'deploy' },
      { id: 'e4', source: 'test', target: 'deploy' },
    ];

    const errors = validateDAG(stages, edges);
    expect(errors).toEqual([]);
  });
});
