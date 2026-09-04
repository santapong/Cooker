import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { notificationTargetsApi, type NotificationEventType, type NotificationKind, type NotificationTarget } from '../api/admin';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import Caps from '../components/ui/Caps';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Check, Field, Select, TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';
import { timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));
const EVENTS: NotificationEventType[] = ['run.succeeded', 'run.failed', 'run.cancelled', 'deploy.succeeded', 'deploy.failed', 'build.failed', 'canary.promoted', 'canary.aborted', 'canary.failed'];

export default function NotificationTargetsPage() {
  const [targets, setTargets] = useState<NotificationTarget[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ name: '', kind: 'webhook' as NotificationKind, url: '', from: '', to: '', enabled: true, events: new Set<NotificationEventType>(['run.failed', 'deploy.failed']) });

  const load = useCallback(async () => {
    try {
      setTargets((await notificationTargetsApi.list()) ?? []);
      setError(null);
    } catch (e) {
      setError(message(e));
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const config =
        form.kind === 'email'
          ? { from: form.from.trim(), to: form.to.split(',').map((s) => s.trim()).filter(Boolean) }
          : form.kind === 'webhook'
            ? { url: form.url.trim() }
            : { webhookUrl: form.url.trim() };
      await notificationTargetsApi.create({ name: form.name.trim(), kind: form.kind, config, eventTypes: Array.from(form.events), enabled: form.enabled });
      pushToast('success', `Target "${form.name.trim()}" created.`);
      setOpen(false);
      setForm({ ...form, name: '', url: '', from: '', to: '' });
      await load();
    } catch (err) {
      pushToast('error', message(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (t: NotificationTarget) => {
    try {
      await notificationTargetsApi.delete(t.id);
      pushToast('info', `Target "${t.name}" removed.`);
      await load();
    } catch (err) {
      pushToast('error', message(err));
    }
  };

  const toggleEvent = (ev: NotificationEventType, on: boolean) => {
    const next = new Set(form.events);
    if (on) next.add(ev);
    else next.delete(ev);
    setForm({ ...form, events: next });
  };

  const rows: ChartRow[] = targets.map((t) => {
    const events = t.eventTypes?.length ?? 0;
    return {
      id: t.id,
      name: t.name,
      sub: t.eventTypes?.join(', '),
      status: t.enabled ? 'ok' : 'idle',
      meta: [t.kind, `${events} ${events === 1 ? 'event' : 'events'}`, t.enabled ? 'enabled' : 'disabled', `updated ${timeAgo(t.updatedAt)}`],
      trailing: <ConfirmButton onConfirm={() => remove(t)}>Remove</ConfirmButton>,
    };
  });

  const newButton = (
    <button type="button" className="hud-btn hud-btn-primary" onClick={() => setOpen((v) => !v)}>
      {open ? 'Cancel' : '＋ New target'}
    </button>
  );

  return (
    <StarChart title="Notifications" count={targets.length} rows={rows} hasThumbs={false} loading={loading} error={error} actions={newButton} empty={{ text: 'No notification targets. Slack, Discord, email and webhook fan-out live here.', action: newButton }}>
      {open && (
        <form className="panel" onSubmit={create}>
          <div className="panel-grid">
            <Field label="Name">
              <TextInput value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="ops channel" required autoFocus />
            </Field>
            <Field label="Kind">
              <Select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as NotificationKind })} options={[{ value: 'webhook', label: 'Webhook' }, { value: 'slack', label: 'Slack' }, { value: 'discord', label: 'Discord' }, { value: 'email', label: 'Email' }]} />
            </Field>
            {form.kind !== 'email' && (
              <Field label={form.kind === 'webhook' ? 'URL' : 'Incoming webhook URL'}>
                <TextInput value={form.url} onChange={(e) => setForm({ ...form, url: e.target.value })} placeholder="https://…" required />
              </Field>
            )}
            {form.kind === 'email' && (
              <>
                <Field label="From">
                  <TextInput value={form.from} onChange={(e) => setForm({ ...form, from: e.target.value })} placeholder="cooker@example.com" />
                </Field>
                <Field label="To" hint="Comma-separated.">
                  <TextInput value={form.to} onChange={(e) => setForm({ ...form, to: e.target.value })} placeholder="ops@example.com" required />
                </Field>
              </>
            )}
          </div>
          <Caps>Events</Caps>
          <div className="option-grid">
            {EVENTS.map((ev) => (
              <Check key={ev} label={ev} checked={form.events.has(ev)} onChange={(on) => toggleEvent(ev, on)} />
            ))}
          </div>
          <Check label="Enabled" checked={form.enabled} onChange={(v) => setForm({ ...form, enabled: v })} />
          <Actions>
            <button type="submit" className="hud-btn hud-btn-primary" disabled={busy || !form.name.trim()}>
              {busy ? 'Creating…' : 'Create target'}
            </button>
          </Actions>
        </form>
      )}
    </StarChart>
  );
}
