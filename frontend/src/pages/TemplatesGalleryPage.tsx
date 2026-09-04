import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { templatesApi, type Template } from '../api/admin';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Check, Field, FormError, TextArea, TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';
import { timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));
const EXAMPLE = '{\n  "stages": [\n    { "id": "build", "name": "build", "type": "build", "config": { "dockerfile": "Dockerfile" } }\n  ],\n  "edges": []\n}';

export default function TemplatesGalleryPage() {
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [form, setForm] = useState({ name: '', description: '', category: '', schema: EXAMPLE, enabled: true });

  const load = useCallback(async () => {
    try {
      setTemplates((await templatesApi.list()) ?? []);
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
    setFormError(null);
    let schema: unknown;
    try {
      schema = JSON.parse(form.schema);
    } catch {
      setFormError('Schema must be valid JSON.');
      return;
    }
    setBusy(true);
    try {
      await templatesApi.create({ name: form.name.trim(), description: form.description.trim() || undefined, category: form.category.trim() || undefined, schema, enabled: form.enabled });
      pushToast('success', `Template "${form.name.trim()}" created.`);
      setOpen(false);
      setForm({ ...form, name: '', description: '', category: '' });
      await load();
    } catch (err) {
      setFormError(message(err));
    } finally {
      setBusy(false);
    }
  };

  const toggle = async (t: Template) => {
    try {
      await templatesApi.update(t.id, { name: t.name, description: t.description, category: t.category, schema: t.schema, iconUrl: t.iconUrl, enabled: !t.enabled });
      await load();
    } catch (err) {
      pushToast('error', message(err));
    }
  };
  const remove = async (t: Template) => {
    try {
      await templatesApi.delete(t.id);
      pushToast('info', `Template "${t.name}" removed.`);
      await load();
    } catch (err) {
      pushToast('error', message(err));
    }
  };

  const rows: ChartRow[] = templates.map((t) => ({
    id: t.id,
    name: t.name,
    sub: t.description,
    status: t.enabled ? 'ok' : 'idle',
    meta: [t.category ?? 'uncategorised', t.enabled ? 'enabled' : 'disabled', `updated ${timeAgo(t.updatedAt)}`],
    trailing: (
      <>
        <button type="button" className="hud-btn" onClick={() => toggle(t)}>
          {t.enabled ? 'Disable' : 'Enable'}
        </button>
        <ConfirmButton onConfirm={() => remove(t)}>Remove</ConfirmButton>
      </>
    ),
  }));

  const newButton = (
    <button type="button" className="hud-btn hud-btn-primary" onClick={() => setOpen((v) => !v)}>
      {open ? 'Cancel' : '＋ New template'}
    </button>
  );

  return (
    <StarChart title="Templates" count={templates.length} rows={rows} hasThumbs={false} loading={loading} error={error} actions={newButton} empty={{ text: 'No templates in the catalog yet. A template seeds a new pipeline with stages and edges.', action: newButton }}>
      {open && (
        <form className="panel" onSubmit={create}>
          <div className="panel-grid">
            <Field label="Name">
              <TextInput value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Go service" required autoFocus />
            </Field>
            <Field label="Category">
              <TextInput value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })} placeholder="backend" />
            </Field>
            <Field label="Description" className="panel-span">
              <TextInput value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} placeholder="Build, scan, push and deploy a Go binary" />
            </Field>
          </div>
          <Field label="Schema (JSON)" hint="Stages and edges in the pipeline shape; ids are re-issued when a pipeline is created from it.">
            <TextArea value={form.schema} onChange={(e) => setForm({ ...form, schema: e.target.value })} style={{ minHeight: 160 }} />
          </Field>
          <Check label="Enabled" checked={form.enabled} onChange={(v) => setForm({ ...form, enabled: v })} />
          <FormError>{formError}</FormError>
          <Actions>
            <button type="submit" className="hud-btn hud-btn-primary" disabled={busy || !form.name.trim()}>
              {busy ? 'Creating…' : 'Create template'}
            </button>
          </Actions>
        </form>
      )}
    </StarChart>
  );
}
