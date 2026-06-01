import type { Node, Edge } from '@xyflow/react';
import type { Stage, PipelineEdge } from '../../types/pipeline';
import type { GroupNodeData } from './nodes/GroupNode';

// Layout constants — mirror the compose viewer's grid spacing
// (composeStore.layoutNodes) so the deployment DAG feels consistent.
const NODE_W = 200;
const NODE_H = 96;
const X_GAP = 90;
const Y_GAP = 48;
const GROUP_PAD = 28;
const GROUP_TITLE_PAD = 18; // extra top padding so nodes clear the title chip
const GROUP_GAP_Y = 60;

type Tone = GroupNodeData['tone'];

// statusToTone collapses a stage-run status into a group tint bucket.
function statusToTone(status: string | undefined): Tone {
  switch (status) {
    case 'running':
      return 'ember';
    case 'failed':
    case 'cancelled':
      return 'bad';
    case 'success':
      return 'good';
    case 'pending':
    default:
      return 'idle';
  }
}

// aggregateTone reduces member statuses to one group tone with a
// severity precedence: any failed → bad; any running → ember; all
// success → good; else idle.
function aggregateTone(statuses: (string | undefined)[]): Tone {
  let anyRunning = false;
  let anyFailed = false;
  let allSuccess = statuses.length > 0;
  for (const s of statuses) {
    if (s === 'failed' || s === 'cancelled') anyFailed = true;
    if (s === 'running') anyRunning = true;
    if (s !== 'success') allSuccess = false;
  }
  if (anyFailed) return 'bad';
  if (anyRunning) return 'ember';
  if (allSuccess) return 'good';
  return 'idle';
}

// topoColumns assigns each stage a column index by longest-path depth
// from a root (a stage with no incoming edge). Used to lay stages left
// to right within their group. Cycles can't occur (validated backend),
// but a defensive visited cap guards against malformed input.
function topoColumns(stageIds: string[], edges: PipelineEdge[]): Map<string, number> {
  const indeg = new Map<string, number>();
  const adj = new Map<string, string[]>();
  for (const id of stageIds) {
    indeg.set(id, 0);
    adj.set(id, []);
  }
  for (const e of edges) {
    if (!indeg.has(e.source) || !indeg.has(e.target)) continue;
    adj.get(e.source)!.push(e.target);
    indeg.set(e.target, (indeg.get(e.target) || 0) + 1);
  }
  const col = new Map<string, number>();
  let frontier = stageIds.filter((id) => (indeg.get(id) || 0) === 0);
  for (const id of frontier) col.set(id, 0);
  let depth = 0;
  const seen = new Set<string>(frontier);
  while (frontier.length > 0 && depth < stageIds.length + 1) {
    const next: string[] = [];
    for (const id of frontier) {
      for (const to of adj.get(id) || []) {
        const cand = (col.get(id) || 0) + 1;
        if (cand > (col.get(to) || 0)) col.set(to, cand);
        if (!seen.has(to)) {
          seen.add(to);
          next.push(to);
        }
      }
    }
    frontier = next;
    depth++;
  }
  // Any stage never reached (shouldn't happen) defaults to column 0.
  for (const id of stageIds) if (!col.has(id)) col.set(id, 0);
  return col;
}

export interface DeploymentLayout {
  nodes: Node[];
  edges: Edge[];
}

// buildDeploymentLayout turns a grouped pipeline (stages with a
// `group`) plus a live status map into xyflow nodes + edges: one
// `group` container node per group with member stage nodes parented to
// it (parentId + relative positions), laid out in topological columns.
// Stages without a group render ungrouped at the top.
//
// statusByStage maps stageId → run status for live tinting (both the
// stage node and the group aggregate).
export function buildDeploymentLayout(
  stages: Stage[],
  edges: PipelineEdge[],
  statusByStage: Record<string, string>,
): DeploymentLayout {
  const col = topoColumns(stages.map((s) => s.id), edges);

  // Bucket stages by group (undefined/"" → "__ungrouped__").
  const UNGROUPED = '__ungrouped__';
  const groups = new Map<string, Stage[]>();
  for (const s of stages) {
    const g = s.group && s.group !== '' ? s.group : UNGROUPED;
    if (!groups.has(g)) groups.set(g, []);
    groups.get(g)!.push(s);
  }

  const nodes: Node[] = [];
  let groupTop = 0;

  // Stable group order: ungrouped first, then alphabetical.
  const groupNames = Array.from(groups.keys()).sort((a, b) => {
    if (a === UNGROUPED) return -1;
    if (b === UNGROUPED) return 1;
    return a.localeCompare(b);
  });

  for (const gname of groupNames) {
    const members = groups.get(gname)!;
    // Column → rows within this group.
    const byCol = new Map<number, Stage[]>();
    for (const s of members) {
      const c = col.get(s.id) || 0;
      if (!byCol.has(c)) byCol.set(c, []);
      byCol.get(c)!.push(s);
    }
    const cols = Array.from(byCol.keys()).sort((a, b) => a - b);
    const maxRows = Math.max(...cols.map((c) => byCol.get(c)!.length), 1);

    const innerW = cols.length * NODE_W + Math.max(cols.length - 1, 0) * X_GAP;
    const innerH = maxRows * NODE_H + Math.max(maxRows - 1, 0) * Y_GAP;
    const grouped = gname !== UNGROUPED;
    const padTop = grouped ? GROUP_PAD + GROUP_TITLE_PAD : 0;
    const padX = grouped ? GROUP_PAD : 0;
    const boxW = innerW + padX * 2;
    const boxH = innerH + padTop + (grouped ? GROUP_PAD : 0);

    const memberStatuses = members.map((s) => statusByStage[s.id]);

    if (grouped) {
      nodes.push({
        id: `group-${gname}`,
        type: 'group',
        position: { x: 0, y: groupTop },
        data: { label: gname, tone: aggregateTone(memberStatuses), count: members.length },
        style: { width: boxW, height: boxH },
        selectable: false,
        draggable: false,
      });
    }

    // Place member stage nodes (relative to the group when grouped).
    for (let ci = 0; ci < cols.length; ci++) {
      const c = cols[ci];
      const rows = byCol.get(c)!;
      for (let ri = 0; ri < rows.length; ri++) {
        const s = rows[ri];
        const relX = padX + ci * (NODE_W + X_GAP);
        const relY = padTop + ri * (NODE_H + Y_GAP);
        const status = statusByStage[s.id];
        const node: Node = {
          id: s.id,
          type: s.type,
          position: grouped ? { x: relX, y: relY } : { x: relX, y: groupTop + relY },
          data: {
            label: s.name,
            config: s.config,
            stageType: s.type,
            status,
            // surface the group + service name for the runtime panel
            group: s.group,
            composeServiceName: s.config?.composeServiceName,
          },
        };
        if (grouped) {
          node.parentId = `group-${gname}`;
          node.extent = 'parent';
        }
        nodes.push(node);
        void statusToTone; // (kept for potential per-node tinting parity)
      }
    }

    groupTop += boxH + GROUP_GAP_Y;
  }

  const flowEdges: Edge[] = edges.map((e) => ({
    id: e.id,
    source: e.source,
    target: e.target,
    animated: statusByStage[e.target] === 'running',
    style: { stroke: '#94a3b8', strokeWidth: 1.6 },
  }));

  return { nodes, edges: flowEdges };
}
