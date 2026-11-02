import { useState } from 'react';
import type { AppModel } from '../../types/app';
import { appsApi } from '../../api/apps';
import { ApiError } from '../../api/client';
import RegistrySelection from '../registry/RegistrySelection';
import { useAuth } from '../../auth/OIDCProvider';

export default function AppRegistryAccess({ app, onSaved }: { app: AppModel; onSaved: () => Promise<void> }) {
  const { user } = useAuth();
  const canEdit = !!user?.roles.some((role) => role === 'admin' || role === 'operator');
  const [draft, setDraft] = useState<{ base: AppModel; ids: string[] } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const stale = !!draft && draft.base.version !== app.version;
  const save = async () => {
    if (!canEdit || !draft?.base.buildPlan || stale) return;
    setBusy(true);
    setError(null);
    try {
      await appsApi.update(app.id, { ...draft.base, buildPlan: { ...draft.base.buildPlan, registryConnectionIds: draft.ids } });
      setDraft(null);
      await onSaved();
    } catch (e) {
      setError(e instanceof ApiError && e.status === 409 ? 'The saved app version changed. Reload selections before retrying' : 'Access or version could not be verified. Check your permissions and reload before retrying');
    } finally {
      setBusy(false);
    }
  };
  if (!app.buildPlan) return <p>Review and save this app’s build plan before selecting registry access.</p>;
  return <>
    <RegistrySelection subject={{ kind: 'app', id: app.id }} value={draft?.ids ?? app.buildPlan.registryConnectionIds ?? []} onChange={(ids) => {
      if (canEdit) setDraft((previous) => ({ base: previous?.base ?? app, ids }));
    }} disabled={!canEdit || busy} />
    {app.deployTarget.kind !== 'docker-host' && <p>Scoped registry deployment currently supports Docker hosts. This target needs a later runtime adapter before it can deploy with this authority.</p>}
    {error && <p role="alert">Registry selections could not be saved: {error}. Your draft is retained.</p>}
    {stale && <p role="alert">This app changed while you were editing. Reload its selections before saving.</p>}
    {canEdit && draft && <div className="form-actions">
      <button type="button" className="hud-btn hud-btn-primary" onClick={() => void save()} disabled={busy || stale}>{busy ? 'Saving…' : 'Save registry selections'}</button>
      <button type="button" className="hud-btn" onClick={() => { setDraft(null); setError(null); void onSaved(); }} disabled={busy}>Reload selections</button>
    </div>}
  </>;
}
