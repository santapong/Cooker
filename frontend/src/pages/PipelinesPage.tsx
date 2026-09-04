import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline, PipelineRun, RunStatus } from '../types/pipeline';
import StarChart, { type ChartRow, type ChartStatus } from '../components/list/StarChart';
import MiniConstellation from '../components/porthole/MiniConstellation';
import { usePortholeTransition } from '../hooks/usePortholeTransition';
import { pushToast } from '../stores/toastStore';
import { shortId, timeAgo } from '../utils/time';

const prefetchEditor = () => void import('./PipelineEditorPage');
const prefetchRun = () => void import('./RunPage');
const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Row status from the latest run: ember while anything runs, green/red when settled. */
export function runChartStatus(run?: PipelineRun): ChartStatus {
  if (!run) return 'idle';
  if (run.status === 'success') return 'ok';
  if (run.status === 'failed') return 'fail';
  if (run.stageRuns?.some((sr) => sr.status === 'running')) return 'running';
  if (run.status === 'running') return 'running';
  return 'idle';
}

function statusMap(run?: PipelineRun): Map<string, RunStatus> {
  const m = new Map<string, RunStatus>();
  for (const sr of run?.stageRuns ?? []) m.set(sr.stageId, sr.status);
  return m;
}

export default function PipelinesPage() {
  const open = usePortholeTransition();
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [latest, setLatest] = useState<Record<string, PipelineRun>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    pipelineApi
      .list({ limit: 100 })
      .then(async (list) => {
        if (cancelled) return;
        setPipelines(list ?? []);
        setLoading(false);
        // Latest run per pipeline — lights the status star and the thumbnail.
        const entries = await Promise.all(
          (list ?? []).map(async (p) => {
            try {
              const runs = await pipelineApi.listRuns(p.id, { limit: 1 });
              return [p.id, runs?.[0]] as const;
            } catch {
              return [p.id, undefined] as const;
            }
          }),
        );
        if (cancelled) return;
        const next: Record<string, PipelineRun> = {};
        for (const [id, run] of entries) if (run) next[id] = run;
        setLatest(next);
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          setError(message(e));
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const create = useCallback(
    async (e: FormEvent) => {
      e.preventDefault();
      const name = newName.trim();
      if (!name) return;
      setBusy(true);
      try {
        const p = await pipelineApi.create({ name, stages: [], edges: [], variables: {} });
        pushToast('success', `Pipeline "${p.name}" created.`);
        open(`/pipelines/${p.id}/edit`, null);
      } catch (err) {
        pushToast('error', message(err));
        setBusy(false);
      }
    },
    [newName, open],
  );

  const rows: ChartRow[] = pipelines.map((p) => {
    const run = latest[p.id];
    const editor = `/pipelines/${p.id}/edit`;
    const runUrl = run ? `/pipelines/${p.id}/runs/${run.id}` : null;
    const stages = p.stages?.length ?? 0;
    return {
      id: p.id,
      name: p.name,
      sub: p.description || undefined,
      status: runChartStatus(run),
      thumb: <MiniConstellation stages={p.stages ?? []} edges={p.edges ?? []} statuses={statusMap(run)} />,
      href: editor,
      onOpen: (el) => open(editor, el),
      onPrefetch: prefetchEditor,
      meta: [
        `${stages} ${stages === 1 ? 'stage' : 'stages'}`,
        `updated ${timeAgo(p.updatedAt)}`,
        run ? `run ${run.status} ${timeAgo(run.finishedAt ?? run.startedAt ?? run.createdAt)}` : 'no runs',
      ],
      trailing: runUrl ? (
        <a
          href={runUrl}
          onMouseEnter={prefetchRun}
          onClick={(ev) => {
            ev.preventDefault();
            ev.stopPropagation();
            open(runUrl, null);
          }}
        >
          run {shortId(run?.id)} ↗
        </a>
      ) : undefined,
    };
  });

  return (
    <StarChart
      title="Pipelines"
      count={pipelines.length}
      rows={rows}
      loading={loading}
      error={error}
      empty={{
        text: 'No pipelines yet. Start a constellation.',
        action: (
          <button type="button" className="hud-btn hud-btn-primary" onClick={() => setCreating(true)}>
            ＋ New pipeline
          </button>
        ),
      }}
      actions={
        creating ? (
          <form className="chart-form" onSubmit={create}>
            <input
              className="input"
              autoFocus
              value={newName}
              placeholder="pipeline name"
              aria-label="New pipeline name"
              onChange={(ev) => setNewName(ev.target.value)}
              disabled={busy}
            />
            <button type="submit" className="hud-btn hud-btn-primary" disabled={busy || !newName.trim()}>
              {busy ? 'Creating…' : 'Create'}
            </button>
            <button type="button" className="hud-btn" onClick={() => setCreating(false)} disabled={busy}>
              Cancel
            </button>
          </form>
        ) : (
          <button type="button" className="hud-btn hud-btn-primary" onClick={() => setCreating(true)} onMouseEnter={prefetchEditor}>
            ＋ New pipeline
          </button>
        )
      }
    />
  );
}
