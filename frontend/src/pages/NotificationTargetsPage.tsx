import { useEffect, useState } from 'react';
import {
  notificationTargetsApi,
  type NotificationEventType,
  type NotificationKind,
  type NotificationTarget,
  type NotificationTargetInput,
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

const KINDS: { value: NotificationKind; label: string; placeholder: string }[] = [
  { value: 'slack', label: 'Slack', placeholder: '{"webhookUrl":"https://hooks.slack.com/services/..."}' },
  { value: 'discord', label: 'Discord', placeholder: '{"webhookUrl":"https://discord.com/api/webhooks/..."}' },
  { value: 'email', label: 'Email (SMTP)', placeholder: '{"host":"smtp.example.com","port":587,"from":"cooker@example.com","to":"oncall@example.com"}' },
  { value: 'webhook', label: 'Generic Webhook', placeholder: '{"url":"https://api.example.com/notify","bearerToken":"..."}' },
];

const EVENT_TYPES: NotificationEventType[] = [
  'run.succeeded',
  'run.failed',
  'run.cancelled',
  'deploy.succeeded',
  'deploy.failed',
  'build.failed',
];

const EMPTY_FORM: NotificationTargetInput = {
  name: '',
  kind: 'slack',
  config: {},
  eventTypes: [],
  enabled: true,
};

/**
 * Admin page for notification targets (Slack / Discord / SMTP / generic
 * Webhook). Talks to GET/POST/PUT/DELETE /api/v1/admin/notification-
 * targets. The dispatcher only fires when COOKER_JOBQUEUE_ENABLED=true
 * on the backend; until then targets can be configured but won't
 * receive events.
 */
export default function NotificationTargetsPage() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [items, setItems] = useState<NotificationTarget[]>([]);
  const [loading, setLoading] = useState(true);
  const [serviceDown, setServiceDown] = useState(false);
  const [editing, setEditing] = useState<NotificationTarget | null>(null);
  const [form, setForm] = useState<NotificationTargetInput>(EMPTY_FORM);
  const [configText, setConfigText] = useState<string>('{}');
  const [submitting, setSubmitting] = useState(false);

  const refresh = () => {
    setLoading(true);
    notificationTargetsApi
      .list()
      .then((rows) => {
        setItems(rows);
        setServiceDown(false);
      })
      .catch((e: Error) => {
        if (/notifier not configured/i.test(e.message)) {
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
    setConfigText('{}');
  };

  const startEdit = (target: NotificationTarget) => {
    setEditing(target);
    setForm({
      name: target.name,
      kind: target.kind,
      config: target.config,
      eventTypes: target.eventTypes ?? [],
      enabled: target.enabled,
    });
    setConfigText(JSON.stringify(target.config, null, 2));
  };

  const toggleEventType = (et: NotificationEventType) => {
    const current = form.eventTypes ?? [];
    if (current.includes(et)) {
      setForm({ ...form, eventTypes: current.filter((x) => x !== et) });
    } else {
      setForm({ ...form, eventTypes: [...current, et] });
    }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    let parsedConfig: unknown;
    try {
      parsedConfig = JSON.parse(configText);
    } catch (err) {
      pushToast({ kind: 'error', message: `Config is not valid JSON: ${(err as Error).message}` });
      return;
    }
    setSubmitting(true);
    try {
      const payload = { ...form, config: parsedConfig };
      if (editing) {
        await notificationTargetsApi.update(editing.id, payload);
        pushToast({ kind: 'success', message: `Updated ${form.name}` });
      } else {
        await notificationTargetsApi.create(payload);
        pushToast({ kind: 'success', message: 'Target created' });
      }
      startNew();
      refresh();
    } catch (err) {
      pushToast({ kind: 'error', message: (err as Error).message });
    } finally {
      setSubmitting(false);
    }
  };

  const remove = async (target: NotificationTarget) => {
    if (!confirm(`Delete target "${target.name}"?`)) return;
    try {
      await notificationTargetsApi.delete(target.id);
      pushToast({ kind: 'success', message: 'Target deleted' });
      refresh();
      if (editing?.id === target.id) startNew();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  const currentKindHint = KINDS.find((k) => k.value === form.kind)?.placeholder ?? '{}';

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="event notifications"
        title="Notification targets"
        subtitle="Slack, Discord, email, or a generic webhook. The notifier dispatches on run + deploy outcomes; configure event filters per target."
        actions={<Btn kind="primary" icon="plus" onClick={startNew}>New target</Btn>}
      />

      {serviceDown ? (
        <Card>
          <EmptyState
            title="Notifier is not configured."
            body="The notifier store is loaded by the jobqueue boot path. Set COOKER_JOBQUEUE_ENABLED=true on the backend to enable it."
          />
        </Card>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 420px', gap: 22 }}>
          <Card pad={0}>
            {loading ? (
              <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
            ) : items.length === 0 ? (
              <EmptyState
                title="No notification targets yet."
                body="Add one on the right. Cooker fires on run.succeeded, run.failed, deploy.succeeded, deploy.failed, and build.failed."
              />
            ) : (
              <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
                {items.map((target) => (
                  <li
                    key={target.id}
                    style={{
                      padding: '14px 18px',
                      borderBottom: `1px solid ${t.line}`,
                      display: 'flex',
                      alignItems: 'center',
                      gap: 14,
                      cursor: 'pointer',
                      background: editing?.id === target.id ? t.surface : 'transparent',
                    }}
                    onClick={() => startEdit(target)}
                  >
                    <div style={{ flex: 1 }}>
                      <div style={{ fontWeight: 600, fontSize: 14 }}>{target.name}</div>
                      <div style={{ color: t.textMute, fontFamily: t.mono, fontSize: 11.5, marginTop: 4 }}>
                        {target.kind} · {(target.eventTypes ?? []).length === 0 ? 'all events' : target.eventTypes!.join(', ')}
                      </div>
                    </div>
                    <Pill tone={target.enabled ? 'good' : 'neutral'}>
                      {target.enabled ? 'enabled' : 'disabled'}
                    </Pill>
                    <Btn
                      kind="danger"
                      onClick={(e) => {
                        e.stopPropagation();
                        remove(target);
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
              {editing ? 'Edit target' : 'Create target'}
            </h3>
            <form onSubmit={submit} style={{ display: 'grid', gap: 12 }}>
              <div>
                <Label>Name</Label>
                <Input
                  required
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                />
              </div>
              <div>
                <Label>Kind</Label>
                <Select
                  value={form.kind}
                  onChange={(e) => setForm({ ...form, kind: e.target.value as NotificationKind })}
                >
                  {KINDS.map((k) => (
                    <option key={k.value} value={k.value}>{k.label}</option>
                  ))}
                </Select>
              </div>
              <div>
                <Label>Config (JSON)</Label>
                <textarea
                  required
                  value={configText}
                  onChange={(e) => setConfigText(e.target.value)}
                  rows={6}
                  placeholder={currentKindHint}
                  style={{
                    width: '100%',
                    padding: '9px 11px',
                    background: t.bg,
                    color: t.text,
                    border: `1px solid ${t.line}`,
                    borderRadius: 7,
                    fontSize: 12,
                    fontFamily: t.mono,
                    outline: 'none',
                    resize: 'vertical',
                  }}
                />
                <div style={{ color: t.textMute, fontSize: 11.5, marginTop: 5 }}>
                  Channel-specific shape — see SECURITY.md / Notifier docs for full schemas.
                </div>
              </div>
              <div>
                <Label>Event types (empty = all)</Label>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 4 }}>
                  {EVENT_TYPES.map((et) => {
                    const active = (form.eventTypes ?? []).includes(et);
                    return (
                      <button
                        key={et}
                        type="button"
                        onClick={() => toggleEventType(et)}
                        style={{
                          padding: '5px 10px',
                          borderRadius: 999,
                          border: `1px solid ${active ? t.accent : t.line}`,
                          background: active ? t.accent : 'transparent',
                          color: active ? '#fff' : t.textSoft,
                          fontFamily: t.mono,
                          fontSize: 11,
                          cursor: 'pointer',
                        }}
                      >
                        {et}
                      </button>
                    );
                  })}
                </div>
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
