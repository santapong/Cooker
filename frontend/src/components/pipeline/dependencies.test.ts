import { describe, expect, it } from 'vitest';
import type { PipelineEdge, Stage } from '../../types/pipeline';
import { connectionProblem, dependencyDrawings, dependencyStory, dependencyViewport, graphBounds, nodeBounds, requirementSummary } from './dependencies';

const stage = (id: string, x: number, y: number): Stage => ({ id, name: id.toUpperCase(), type: 'build', position: { x, y }, config: {} });
const stages = [stage('build', 20, 20), stage('test', 340, 20), stage('deploy', 660, 160)];
const edge: PipelineEdge = { id: 'build-test', source: 'build', target: 'test' };

describe('authored dependency explanations', () => {
  it('treats an omitted condition as success and points from prerequisite to waiting step', () => {
    expect(dependencyStory(edge, stages)).toMatchObject({ source: 'BUILD', target: 'TEST', condition: 'success', summary: 'TEST waits until BUILD succeeds.' });
  });
  it('keeps failure and always distinct without inventing service-health semantics', () => {
    expect(dependencyStory({ ...edge, condition: 'failure' }, stages).summary).toBe('TEST waits until BUILD fails.');
    expect(dependencyStory({ ...edge, condition: 'always' }, stages).summary).toBe('TEST waits until BUILD finishes or is skipped.');
    expect(dependencyStory({ ...edge, condition: 'always' }, stages).reason).toContain('succeeded, failed, or was skipped');
    expect(dependencyStory(edge, stages).reason).not.toMatch(/healthy|traffic/);
  });
  it('explains roots and all incoming requirements at a join', () => {
    expect(requirementSummary(0)).toContain('can start first');
    expect(requirementSummary(1)).toBe('Waits for 1 requirement');
    expect(requirementSummary(3)).toBe('Waits for all 3 requirements');
  });
  it('exposes broken references instead of silently describing a different step', () => {
    expect(dependencyStory({ ...edge, target: 'missing' }, stages).target).toBe('Missing step (missing)');
  });
});

describe('connection admission', () => {
  const edges = [edge, { id: 'test-deploy', source: 'test', target: 'deploy' }];
  it('rejects self references, unknown endpoints, duplicate edges and transitive cycles', () => {
    expect(connectionProblem(stages, edges, 'build', 'build')).toMatch(/itself/);
    expect(connectionProblem(stages, edges, 'missing', 'test')).toMatch(/exist/);
    expect(connectionProblem(stages, edges, 'build', 'test')).toMatch(/already/);
    expect(connectionProblem(stages, edges, 'deploy', 'build')).toMatch(/cycle/);
  });
  it('allows an additional prerequisite without mutating real stage or edge IDs', () => {
    const before = JSON.stringify({ stages, edges });
    expect(connectionProblem(stages, edges, 'build', 'deploy')).toBeNull();
    expect(JSON.stringify({ stages, edges })).toBe(before);
  });
});

describe('dependency geometry and complete fit bounds', () => {
  const branches = [stage('a', 20, 0), stage('b', 20, 200), stage('c', 380, 100), stage('d', 740, 100)];
  const edges = [{ id: 'ac', source: 'a', target: 'c' }, { id: 'bc', source: 'b', target: 'c' }, { id: 'ad', source: 'a', target: 'd' }, { id: 'bd', source: 'b', target: 'd' }, { id: 'cd', source: 'c', target: 'd' }];
  it('gives each join/fork its own port and a stable dependency number', () => {
    const drawings = dependencyDrawings(branches, edges);
    expect(drawings.map((drawing) => drawing.number)).toEqual([1, 2, 3, 4, 5]);
    expect(drawings[0].source.y).not.toBe(drawings[2].source.y);
    expect(drawings[0].target.y).not.toBe(drawings[1].target.y);
    expect(drawings[0].source.x).toBeGreaterThan(branches[0].position.x);
    expect(drawings[0].target.x).toBe(branches[2].position.x);
  });
  it.each([1, 0.4, 0.12])('keeps every node, curved path bound and 44px number inside fit at zoom %s', (zoom) => {
    const bounds = graphBounds(branches, edges, zoom);
    const inside = (x: number, y: number) => x >= bounds.x-0.01 && y >= bounds.y-0.01 && x <= bounds.x+bounds.width+0.01 && y <= bounds.y+bounds.height+0.01;
    for (const node of branches.map((item) => nodeBounds(item, edges))) {
      expect(inside(node.x, node.y)).toBe(true);
      expect(inside(node.x+node.width, node.y+node.height)).toBe(true);
    }
    const drawings = dependencyDrawings(branches, edges, zoom);
    for (const drawing of drawings) {
      expect(inside(drawing.bounds.x, drawing.bounds.y)).toBe(true);
      expect(inside(drawing.bounds.x+drawing.bounds.width, drawing.bounds.y+drawing.bounds.height)).toBe(true);
      expect(inside(drawing.label.x-22/zoom, drawing.label.y-22/zoom)).toBe(true);
      expect(inside(drawing.label.x+22/zoom, drawing.label.y+22/zoom)).toBe(true);
    }
    for (let i=0; i<drawings.length; i++) for (let j=i+1; j<drawings.length; j++) {
      const a=drawings[i].label; const b=drawings[j].label;
      expect(Math.abs(a.x-b.x) >= 44/zoom || Math.abs(a.y-b.y) >= 44/zoom).toBe(true);
    }
  });
  it('handles empty graphs and missing endpoints without invented geometry', () => {
    expect(graphBounds([], []).width).toBeGreaterThan(0);
    expect(dependencyDrawings(branches, [{ id: 'bad', source: 'a', target: 'unknown' }])).toEqual([]);
  });
  it.each([[1200, 580], [620, 480], [280, 480]])('fits physical label hit targets on a dense map at %s×%s', (width, height) => {
    const manyStages = Array.from({ length: 22 }, (_, i) => stage(`step-${i}`, Math.floor(i/4)*350, (i%4)*180));
    const manyEdges: PipelineEdge[] = manyStages.slice(4).flatMap((item, i) => [
      { id: `forward-${i}`, source: manyStages[i].id, target: item.id },
      ...(i < 11 ? [{ id: `join-${i}`, source: manyStages[(i+1)%4].id, target: item.id }] : []),
    ]);
    const view = dependencyViewport(manyStages, manyEdges, width, height);
    for (const drawing of dependencyDrawings(manyStages, manyEdges, view.zoom, width * 0.78)) {
      const x = drawing.label.x*view.zoom+view.x; const y = drawing.label.y*view.zoom+view.y;
      expect(x-22).toBeGreaterThanOrEqual(0);
      expect(x+22).toBeLessThanOrEqual(width);
      expect(y-22).toBeGreaterThanOrEqual(0);
      expect(y+22).toBeLessThanOrEqual(height);
    }
  });
});
