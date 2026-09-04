import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { schedulesApi, type Schedule } from '../api/admin';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline } from '../types/pipeline';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Check, Field, FormError, Select, TextInput } from '../components/ui/form';
import { usePortholeTransition } from '../hooks/usePortholeTransition';
import { pushToast } from '../stores/toastStore';
import { shortId, timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export default function SchedulesPage() {
  const open = usePortholeTransition();
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [form, setForm] = useState({ pipelineId: '', name: '', cronExpr: '0 3 * * *', timezone: 'UTC', enabled: true });

  const load = useCallback(async () => {
    const [s, p] = await Promise.allSettled([schedulesApi.list(), pipelineApi.list({ limit: 100 })]);
    if (s.status === 'fulfilled') {
      setSchedules(s.value ?? []);
      setError(null);
    } else setError(message(s.reason));
    if (p.status === 'fulfilled') {
      setPipelines(p.value ?? []);
      setForm((f) => (f.pipelineId || !p.value?.length ? f : { ...f, pipelineId: p.value[0].id }));
    }
    setLoading(false);
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setBusy(true);
    try {
      await schedulesApi.create({ pipelineId: form.pipelineId, name: form.name.trim() || undefined, cronExpr: form.cronExpr.trim(), timezone: form.timezone.trim() || 'UTC', enabled: form.enabled });
      pushToast('success', 'Schedule created.');
      setFormOpen(false);
      await load();
    } catch (err) {
      setFormError(message(err));
    } finally {
      setBusy(false);
    }
  };
  const remove = async (s: Schedule) => {
    try {
      await schedulesApi.delete(s.id);
      pushToast('info', 'Schedule removed.');
      await load();
    } catch (err) {
      pushToast('error', message(err));
    }
  };

  const nameOf = (id: string) => pipelines.find((p) => p.id === id)?.name ?? shortId(id);
  const rows: ChartRow[] = schedules.map((s) => {
    const runUrl = s.lastRunId ? `/pipelines/${s.pipelineId}/runs/${s.lastRunId}` : null;
    return {
      id: s.id,
      name: s.name || s.cronExpr,
      sub: `pipeline ${nameOf(s.pipelineId)}`,
      status: s.enabled ? 'ok' : 'idle',
      meta: [s.cronExpr, s.timezone, `next ${timeAgo(s.nextRunAt)}`, ...(s.lastRunAt ? [`last ${timeAgo(s.lastRunAt)}`] : [])],
      trailing: (
        <>
          {runUrl && (
            <a
              href={runUrl}
              onClick={(ev) => {
                ev.preventDefault();
                open(runUrl, null);
              }}
            >
              last run ↗
            </a>
          )}
          <ConfirmButton onConfirm={() => remove(s)}>Remove</ConfirmButton>
        </>
      ),
    };
  });

  const newButton = (
    <button type="button" className="hud-btn hud-btn-primary" onClick={() => setFormOpen((v) => !v)} disabled={!pipelines.length}>
      {formOpen ? 'Cancel' : '＋ New schedule'}
    </button>
  );

  return (
    <StarChart title="Schedules" count={schedules.length} rows={rows} hasThumbs={false} loading={loading} error={error} actions={newButton} empty={{ text: 'No schedules. Cron-triggered runs appear here once the scheduler feature is enabled.', action: newButton }}>
      {formOpen && (
        <form className="panel" onSubmit={create}>
          <div className="panel-grid">
            <Field label="Pipeline">
              <Select value={form.pipelineId} onChange={(e) => setForm({ ...form, pipelineId: e.target.value })} options={pipelines.map((p) => ({ value: p.id, label: p.name }))} />
            </Field>
            <Field label="Name" hint="Optional.">
              <TextInput value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="nightly" />
            </Field>
            <Field label="Cron" hint="POSIX five-field expression.">
              <TextInput value={form.cronExpr} onChange={(e) => setForm({ ...form, cronExpr: e.target.value })} placeholder="0 3 * * *" required />
            </Field>
            <Field label="Timezone" hint="IANA name; DST-correct.">
              <TextInput value={form.timezone} onChange={(e) => setForm({ ...form, timezone: e.target.value })} placeholder="Europe/Berlin" />
            </Field>
          </div>
          <Check label="Enabled" checked={form.enabled} onChange={(v) => setForm({ ...form, enabled: v })} />
          <FormError>{formError}</FormError>
          <Actions>
            <button type="submit" className="hud-btn hud-btn-primary" disabled={busy || !form.pipelineId || !form.cronExpr.trim()}>
              {busy ? 'Creating…' : 'Create schedule'}
            </button>
          </Actions>
        </form>
      )}
    </StarChart>
  );
}
