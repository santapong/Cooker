import { useEffect, useState } from 'react';
import {
  schedulesApi,
  type Schedule,
  type ScheduleInput,
} from '../api/admin';
import { useTheme } from '../theme/ThemeProvider';
import {
  Btn,
  Card,
  EmptyState,
  Input,
  Label,
  PageHeader,
  Pill,
  Select,
} from '../components/ui/atoms';
import { useToastStore } from '../stores/toastStore';

// Common IANA zones operators are most likely to want. The backend
// accepts any zone the Go runtime knows; this is just the convenience
// list rendered in the dropdown.
const TZ_OPTIONS = [
  'UTC',
  'Asia/Bangkok',
  'Asia/Tokyo',
  'Asia/Singapore',
  'Europe/London',
  'Europe/Berlin',
  'Europe/Paris',
  'America/New_York',
  'America/Los_Angeles',
];

const EMPTY_FORM: ScheduleInput = {
  pipelineId: '',
  name: '',
  cronExpr: '0 * * * *',
  timezone: 'UTC',
  enabled: true,
};

/**
 * Admin page for cron-triggered pipeline runs. Lists every schedule
 * in the catalog and exposes a create/edit form. Talks to
 * GET/POST/PUT/DELETE /api/v1/admin/schedules.
 *
 * The page renders 503-friendly: when the scheduler is disabled the
 * backend returns 503 and the list shows an empty-state with a hint.
 */
export default function SchedulesPage() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [items, setItems] = useState<Schedule[]>([]);
  const [loading, setLoading] = useState(true);
  const [serviceDown, setServiceDown] = useState(false);
  const [editing, setEditing] = useState<Schedule | null>(null);
  const [form, setForm] = useState<ScheduleInput>(EMPTY_FORM);
  const [submitting, setSubmitting] = useState(false);

  const refresh = () => {
    setLoading(true);
    schedulesApi
      .list()
      .then((rows) => {
        setItems(rows);
        setServiceDown(false);
      })
      .catch((e: Error) => {
        if (/scheduler not configured/i.test(e.message)) {
          setServiceDown(true);
        } else {
          pushToast({ kind: 'error', message: e.message });
        }
        setItems([]);
      })
      .finally(() => setLoading(false));
  };

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(refresh, []);

  const startNew = () => {
    setEditing(null);
    setForm(EMPTY_FORM);
  };

  const startEdit = (s: Schedule) => {
    setEditing(s);
    setForm({
      pipelineId: s.pipelineId,
      name: s.name ?? '',
      cronExpr: s.cronExpr,
      timezone: s.timezone,
      enabled: s.enabled,
    });
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      if (editing) {
        await schedulesApi.update(editing.id, form);
        pushToast({ kind: 'success', message: `Updated schedule ${form.name || form.pipelineId}` });
      } else {
        await schedulesApi.create(form);
        pushToast({ kind: 'success', message: 'Schedule created' });
      }
      startNew();
      refresh();
    } catch (err) {
      pushToast({ kind: 'error', message: (err as Error).message });
    } finally {
      setSubmitting(false);
    }
  };

  const remove = async (s: Schedule) => {
    if (!confirm(`Delete schedule "${s.name || s.id}"?`)) return;
    try {
      await schedulesApi.delete(s.id);
      pushToast({ kind: 'success', message: 'Schedule deleted' });
      refresh();
      if (editing?.id === s.id) startNew();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="cron triggers"
        title="Schedules"
        subtitle="Schedule pipelines to run on a cron expression. The scheduler must be enabled in your deployment (COOKER_SCHEDULER_ENABLED=true)."
        actions={<Btn kind="primary" icon="plus" onClick={startNew}>New schedule</Btn>}
      />

      {serviceDown ? (
        <Card>
          <EmptyState
            title="Scheduler is not configured."
            body="Set COOKER_SCHEDULER_ENABLED=true (and COOKER_JOBQUEUE_ENABLED=true) on the backend, then restart. Operators can still seed schedules via SQL until then."
          />
        </Card>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 380px', gap: 22 }}>
          <Card pad={0}>
            {loading ? (
              <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
            ) : items.length === 0 ? (
              <EmptyState
                title="No schedules yet."
                body="Create one on the right. Cron expressions use the standard 5-field POSIX format."
              />
            ) : (
              <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
                {items.map((s) => (
                  <li
                    key={s.id}
                    style={{
                      padding: '14px 18px',
                      borderBottom: `1px solid ${t.line}`,
                      display: 'flex',
                      alignItems: 'center',
                      gap: 14,
                      cursor: 'pointer',
                      background: editing?.id === s.id ? t.surface : 'transparent',
                    }}
                    onClick={() => startEdit(s)}
                  >
                    <div style={{ flex: 1 }}>
                      <div style={{ fontWeight: 600, fontSize: 14 }}>
                        {s.name || s.pipelineId}
                      </div>
                      <div style={{ color: t.textMute, fontFamily: t.mono, fontSize: 12, marginTop: 4 }}>
                        <code>{s.cronExpr}</code> · {s.timezone} · next {new Date(s.nextRunAt).toLocaleString()}
                      </div>
                    </div>
                    <Pill tone={s.enabled ? 'good' : 'neutral'}>
                      {s.enabled ? 'enabled' : 'disabled'}
                    </Pill>
                    <Btn
                      kind="danger"
                      onClick={(e) => {
                        e.stopPropagation();
                        remove(s);
                      }}
                    >
                      Delete
                    </Btn>
                  </li>
                ))}
              </ul>
            )}
          </Card>

          <Card>
            <h3 style={{ margin: '0 0 14px', fontSize: 15 }}>
              {editing ? 'Edit schedule' : 'Create schedule'}
            </h3>
            <form onSubmit={submit} style={{ display: 'grid', gap: 12 }}>
              <div>
                <Label>Pipeline ID</Label>
                <Input
                  required
                  value={form.pipelineId}
                  onChange={(e) => setForm({ ...form, pipelineId: e.target.value })}
                  placeholder="00000000-0000-0000-0000-000000000000"
                  style={{ fontFamily: t.mono, fontSize: 12 }}
                />
              </div>
              <div>
                <Label>Name (optional)</Label>
                <Input
                  value={form.name ?? ''}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                />
              </div>
              <div>
                <Label>Cron expression</Label>
                <Input
                  required
                  value={form.cronExpr}
                  onChange={(e) => setForm({ ...form, cronExpr: e.target.value })}
                  placeholder="0 * * * *"
                  style={{ fontFamily: t.mono, fontSize: 12 }}
                />
                <div style={{ color: t.textMute, fontSize: 11.5, marginTop: 5 }}>
                  Standard 5-field POSIX cron — minute hour dom month dow.
                </div>
              </div>
              <div>
                <Label>Timezone</Label>
                <Select
                  value={form.timezone ?? 'UTC'}
                  onChange={(e) => setForm({ ...form, timezone: e.target.value })}
                >
                  {TZ_OPTIONS.map((tz) => (
                    <option key={tz} value={tz}>{tz}</option>
                  ))}
                </Select>
              </div>
              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  color: t.text,
                  fontSize: 13,
                }}
              >
                <input
                  type="checkbox"
                  checked={form.enabled ?? true}
                  onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
                />
                Enabled
              </label>
              <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                <Btn kind="primary" type="submit" disabled={submitting}>
                  {editing ? 'Update' : 'Create'}
                </Btn>
                {editing && (
                  <Btn kind="ghost" type="button" onClick={startNew}>
                    Cancel
                  </Btn>
                )}
              </div>
            </form>
          </Card>
        </div>
      )}
    </div>
  );
}
