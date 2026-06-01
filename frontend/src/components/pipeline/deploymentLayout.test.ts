import { describe, it, expect } from 'vitest';
import { buildDeploymentLayout } from './deploymentLayout';
import type { Stage, PipelineEdge } from '../../types/pipeline';

function stage(id: string, type: Stage['type'], group?: string, svc?: string): Stage {
  return {
    id,
    name: id,
    type,
    config: svc ? { composeServiceName: svc } : {},
    position: { x: 0, y: 0 },
    group,
  };
}

describe('buildDeploymentLayout', () => {
  const stages: Stage[] = [
    stage('build-web', 'build', 'frontend', 'web'),
    stage('push-web', 'push', 'frontend', 'web'),
    stage('deploy-web', 'deploy', 'frontend', 'web'),
    stage('deploy-db', 'deploy', 'backend', 'db'),
  ];
  const edges: PipelineEdge[] = [
    { id: 'e1', source: 'build-web', target: 'push-web' },
    { id: 'e2', source: 'push-web', target: 'deploy-web' },
    { id: 'e3', source: 'deploy-db', target: 'deploy-web' },
  ];

  it('emits one group container per group with member parentId set', () => {
    const { nodes } = buildDeploymentLayout(stages, edges, {});
    const groups = nodes.filter((n) => n.type === 'group');
    expect(groups.map((g) => g.id).sort()).toEqual(['group-backend', 'group-frontend']);

    const web = nodes.find((n) => n.id === 'build-web')!;
    expect(web.parentId).toBe('group-frontend');
    expect(web.extent).toBe('parent');

    const db = nodes.find((n) => n.id === 'deploy-db')!;
    expect(db.parentId).toBe('group-backend');
  });

  it('lays members out in topological columns (relative x increases with depth)', () => {
    const { nodes } = buildDeploymentLayout(stages, edges, {});
    const b = nodes.find((n) => n.id === 'build-web')!;
    const p = nodes.find((n) => n.id === 'push-web')!;
    const d = nodes.find((n) => n.id === 'deploy-web')!;
    expect(b.position.x).toBeLessThan(p.position.x);
    expect(p.position.x).toBeLessThan(d.position.x);
  });

  it('aggregates group tone from member statuses (any failed → bad)', () => {
    const { nodes } = buildDeploymentLayout(stages, edges, {
      'build-web': 'success',
      'push-web': 'failed',
      'deploy-web': 'pending',
    });
    const fe = nodes.find((n) => n.id === 'group-frontend')!;
    expect((fe.data as { tone?: string }).tone).toBe('bad');
  });

  it('group tone is good when all members succeed', () => {
    const { nodes } = buildDeploymentLayout(stages, edges, {
      'deploy-db': 'success',
    });
    const be = nodes.find((n) => n.id === 'group-backend')!;
    expect((be.data as { tone?: string }).tone).toBe('good');
  });

  it('carries edges through with the same ids', () => {
    const { edges: out } = buildDeploymentLayout(stages, edges, {});
    expect(out.map((e) => e.id).sort()).toEqual(['e1', 'e2', 'e3']);
  });

  it('ungrouped stages render without a parent', () => {
    const ung: Stage[] = [stage('solo', 'deploy', undefined, 'solo')];
    const { nodes } = buildDeploymentLayout(ung, [], {});
    expect(nodes.filter((n) => n.type === 'group')).toHaveLength(0);
    expect(nodes[0].parentId).toBeUndefined();
  });
});
