import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  templatesApi,
  type Template,
  type TemplateInput,
} from '../api/admin';
import { post } from '../api/client';
import { useTheme } from '../theme/ThemeProvider';
import {
  Btn,
  Card,
  EmptyState,
  Input,
  Label,
  PageHeader,
  Pill,
} from '../components/ui/atoms';
import { ConstellationThumb } from '../components/ui/ConstellationThumb';
import { hexA } from '../theme/tokens';
import { useToastStore } from '../stores/toastStore';

// best-effort extraction of stage kinds from a template's Pipeline-shaped schema
function templateKinds(tpl: Template): string[] {
  const stages = (tpl.schema as { stages?: Array<{ type?: string }> } | undefined)?.stages;
  return Array.isArray(stages) ? stages.map((s) => s.type ?? 'custom') : [];
}

const EMPTY_FORM: TemplateInput = {
  name: '',
  description: '',
  category: '',
  schema: { stages: [], edges: [], variables: {} },
  iconUrl: '',
  enabled: true,
};

interface CreatePipelineResponse {
  templateId: string;
  pipeline: { id: string };
}

/**
 * Catalog gallery for pipeline templates (Phase-2 / F4). Every
 * authenticated user can browse the catalog (GET /api/v1/templates is
 * not gated to admin), and clicking a card materialises a new pipeline
 * via POST /api/v1/pipelines/from-template/:id. Admin users see an
 * "Edit catalog" pane that talks to the /admin/templates CRUD
 * endpoints; non-admins simply don't see the form (the backend would
 * 403 anyway).
 */
export default function TemplatesGalleryPage() {
  const t = useTheme();
  const nav = useNavigate();
  const pushToast = useToastStore((s) => s.push);
  const [items, setItems] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [serviceDown, setServiceDown] = useState(false);
  const [editing, setEditing] = useState<Template | null>(null);
  const [form, setForm] = useState<TemplateInput>(EMPTY_FORM);
  const [schemaText, setSchemaText] = useState<string>(JSON.stringify(EMPTY_FORM.schema, null, 2));
  const [submitting, setSubmitting] = useState(false);
  const [useFromName, setUseFromName] = useState<Record<string, string>>({});

  const refresh = () => {
    setLoading(true);
    templatesApi
      .list()
      .then((rows) => {
        setItems(rows);
        setServiceDown(false);
      })
      .catch((e: Error) => {
        if (/templates store not configured/i.test(e.message)) {
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
    setSchemaText(JSON.stringify(EMPTY_FORM.schema, null, 2));
  };

  const startEdit = (tpl: Template) => {
    setEditing(tpl);
    setForm({
      name: tpl.name,
      description: tpl.description ?? '',
      category: tpl.category ?? '',
      schema: tpl.schema,
      iconUrl: tpl.iconUrl ?? '',
      enabled: tpl.enabled,
    });
    setSchemaText(JSON.stringify(tpl.schema, null, 2));
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    let parsedSchema: unknown;
    try {
      parsedSchema = JSON.parse(schemaText);
    } catch (err) {
      pushToast({ kind: 'error', message: `Schema is not valid JSON: ${(err as Error).message}` });
      return;
    }
    setSubmitting(true);
    try {
      const payload = { ...form, schema: parsedSchema };
      if (editing) {
        await templatesApi.update(editing.id, payload);
        pushToast({ kind: 'success', message: `Updated template ${form.name}` });
      } else {
        await templatesApi.create(payload);
        pushToast({ kind: 'success', message: `Template "${form.name}" added` });
      }
      startNew();
      refresh();
    } catch (err) {
      pushToast({ kind: 'error', message: (err as Error).message });
    } finally {
      setSubmitting(false);
    }
  };

  const remove = async (tpl: Template) => {
    if (!confirm(`Delete template "${tpl.name}"?`)) return;
    try {
      await templatesApi.delete(tpl.id);
      pushToast({ kind: 'success', message: 'Template deleted' });
      refresh();
      if (editing?.id === tpl.id) startNew();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  const instantiateTemplate = async (tpl: Template) => {
    const name = (useFromName[tpl.id] ?? '').trim();
    if (!name) {
      pushToast({ kind: 'error', message: 'Pick a pipeline name first.' });
      return;
    }
    try {
      const res = await post<CreatePipelineResponse>(`/pipelines/from-template/${tpl.id}`, {
        name,
        description: tpl.description,
      });
      pushToast({ kind: 'success', message: `Created pipeline "${name}"` });
      nav(`/pipelines/${res.pipeline.id}/edit`);
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="catalog"
        title="Pipeline templates"
        subtitle="Browse curated pipelines and instantiate a fresh copy. Admin users can edit the catalog on the right."
        actions={<Btn kind="primary" icon="plus" onClick={startNew}>New template</Btn>}
      />

      {serviceDown ? (
        <Card>
          <EmptyState
            title="Templates catalog is not configured."
            body="Set DATABASE_URL on the backend so the pipeline_templates table is reachable. Operators can also seed templates via SQL until the catalog is in use."
          />
        </Card>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 420px', gap: 22 }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 16 }}>
            {loading ? (
              <Card>
                <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
              </Card>
            ) : items.length === 0 ? (
              <Card>
                <EmptyState
                  title="No templates yet."
                  body="Add one on the right — describe a Pipeline-shaped JSON schema and operators can spin up new pipelines from it in one click."
                />
              </Card>
            ) : (
              items.map((tpl) => (
                <Card key={tpl.id} pad={18} style={{ position: 'relative', overflow: 'hidden' }}>
                  {/* nebula corner glow */}
                  <div
                    style={{
                      position: 'absolute',
                      top: -40,
                      right: -40,
                      width: 130,
                      height: 130,
                      borderRadius: 999,
                      background: `radial-gradient(circle, ${hexA(t.cyan, 0.16)} 0%, transparent 70%)`,
                      pointerEvents: 'none',
                    }}
                  />
                  <div style={{ position: 'relative', display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 10, marginBottom: 8 }}>
                    <h4 style={{ margin: 0, fontFamily: t.display, fontSize: 16, fontWeight: 600, letterSpacing: -0.3 }}>
                      {tpl.name}
                    </h4>
                    <Pill tone={tpl.enabled ? 'good' : 'neutral'}>
                      {tpl.enabled ? 'live' : 'draft'}
                    </Pill>
                  </div>
                  {tpl.category && (
                    <div style={{ position: 'relative', marginBottom: 8 }}>
                      <Pill tone="accent">{tpl.category}</Pill>
                    </div>
                  )}
                  <div style={{ position: 'relative', margin: '4px -4px 10px' }}>
                    <ConstellationThumb kinds={templateKinds(tpl)} height={72} />
                  </div>
                  {tpl.description && (
                    <p style={{ position: 'relative', color: t.textSoft, fontSize: 13, lineHeight: 1.5, margin: '0 0 12px' }}>
                      {tpl.description}
                    </p>
                  )}
                  <div style={{ position: 'relative', display: 'grid', gap: 8 }}>
                    <Input
                      placeholder="New pipeline name…"
                      value={useFromName[tpl.id] ?? ''}
                      onChange={(e) => setUseFromName({ ...useFromName, [tpl.id]: e.target.value })}
                    />
                    <div style={{ display: 'flex', gap: 8 }}>
                      <Btn kind="primary" onClick={() => instantiateTemplate(tpl)}>
                        Use template
                      </Btn>
                      <Btn kind="ghost" onClick={() => startEdit(tpl)}>
                        Edit
                      </Btn>
                      <Btn kind="danger" onClick={() => remove(tpl)}>
                        Delete
                      </Btn>
                    </div>
                  </div>
                </Card>
              ))
            )}
          </div>

          <Card>
            <h3 style={{ margin: '0 0 14px', fontSize: 15 }}>
              {editing ? 'Edit template' : 'Create template'}
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
                <Label>Category</Label>
                <Input
                  value={form.category ?? ''}
                  onChange={(e) => setForm({ ...form, category: e.target.value })}
                  placeholder="ci · cd · platform · sample"
                />
              </div>
              <div>
                <Label>Description</Label>
                <Input
                  value={form.description ?? ''}
                  onChange={(e) => setForm({ ...form, description: e.target.value })}
                />
              </div>
              <div>
                <Label>Icon URL</Label>
                <Input
                  value={form.iconUrl ?? ''}
                  onChange={(e) => setForm({ ...form, iconUrl: e.target.value })}
                  placeholder="optional"
                />
              </div>
              <div>
                <Label>Schema (Pipeline JSON)</Label>
                <textarea
                  required
                  value={schemaText}
                  onChange={(e) => setSchemaText(e.target.value)}
                  rows={10}
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
                  Pipeline-shaped JSON. New pipelines get fresh stage IDs and are re-validated through
                  ValidatePipelineDAG on instantiation.
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
                Enabled (appears in gallery)
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
