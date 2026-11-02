import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import type { AppModel } from '../../types/app';
import { appsApi } from '../../api/apps';
import { catalogApi } from '../../api/catalog';
import { useAuth } from '../../auth/OIDCProvider';
import { useConfigurationDraftStore } from '../../stores/configurationDraftStore';
import { configurationError } from '../../utils/environmentEditing';
import ServiceEnvironmentSelector from '../environment/ServiceEnvironmentSelector';
import { useDraftMutationFence } from '../environment/useDraftMutationFence';
import { useEnvironmentInventory } from '../environment/useEnvironmentInventory';
import { Actions, Field, Select } from '../ui/form';
import ConfirmButton from '../ui/ConfirmButton';

interface BindingDraft { base: AppModel; environmentId: string; bindings: Record<string, string[]>; scope: 'service' | '' }
export default function AppEnvironmentBindings({ app, onSaved }: { app: AppModel; onSaved: () => Promise<void> }) {
  const { user } = useAuth();
  const owner = user?.sub ?? '';
  const canEdit = !!user?.roles.some((role) => role === 'admin' || role === 'operator');
  const drafts = useConfigurationDraftStore();
  const currentMutation = useDraftMutationFence();
  const key = `app:${app.id}:environment`;
  const draft = (drafts.owner === owner ? drafts.drafts[key] : undefined) as BindingDraft | undefined;
  const value = draft ?? { base: app, environmentId: app.environmentId ?? '', bindings: app.buildPlan?.serviceEnvironment ?? {}, scope: app.buildPlan?.environmentScope ?? '' };
  const inventory = useEnvironmentInventory(value.environmentId);
  const [services, setServices] = useState<string[]>([]);
  const [servicesUnavailable, setServicesUnavailable] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const dockerfile = app.buildPlan?.kind === 'dockerfile';
  const invalidServices = dockerfile ? Object.keys(value.bindings).filter((name) => name !== 'deploy') : [];
  useEffect(() => { setBusy(false); setError(null); setNotice(null); }, [currentMutation]);
  useEffect(() => { let active = true; setServices([]); setServicesUnavailable(false); if (app.buildPlan?.kind !== 'compose') return; void catalogApi.services({ appId: app.id, management: 'cooker', limit: 100 }).then((result) => { if (active) { setServices(result.items.filter((service) => service.management === 'cooker' && service.nativeServiceId).map((service) => service.nativeServiceId!)); setServicesUnavailable(result.total > result.items.length || result.sources.some((source) => !source.available)); } }).catch(() => { if (active) setServicesUnavailable(true); }); return () => { active = false; }; }, [app.id, app.buildPlan?.kind]);
  const stale = !!draft && draft.base.version !== app.version;
  const change = (patch: Partial<BindingDraft>) => { if (canEdit && !busy) { drafts.setDraft(owner, key, { ...value, ...patch }); setNotice(null); } };
  const save = async () => {
    if (!canEdit || !draft?.base.buildPlan || busy || stale || invalidServices.length || draft.base.version === undefined) return;
    const generation = drafts.generation;
    setBusy(true); setError(null); setNotice(null);
    try {
      await appsApi.update(app.id, { ...draft.base, environmentId: draft.environmentId, buildPlan: { ...draft.base.buildPlan, environmentScope: draft.scope, serviceEnvironment: draft.bindings } });
      drafts.clearDraftIf(owner, key, draft, generation); if (!currentMutation(generation)) return; setNotice('Service bindings saved. The next reviewed deployment uses them; saving does not deploy.'); await onSaved();
    } catch (failure) { if (currentMutation(generation)) setError(configurationError(failure)); } finally { if (currentMutation(generation)) setBusy(false); }
  };
  if (app.buildPlan?.kind !== 'compose' && !dockerfile) return <p>Explicit runtime-key selection supports Compose and Dockerfile apps. Buildpack apps keep their existing linked-environment behavior; native supported DAG steps can select keys individually.</p>;
  return <>
    <Field label="Linked runtime environment"><Select disabled={!canEdit || busy} value={value.environmentId} onChange={(event) => change({ environmentId: event.target.value })} options={[{ value: '', label: 'No linked environment' }, ...(!inventory.environments.some((environment) => environment.id === value.environmentId) && value.environmentId ? [{ value: value.environmentId, label: `${value.environmentId} · not in loaded inventory` }] : []), ...inventory.environments.map((environment) => ({ value: environment.id, label: environment.name }))]} /></Field>
    {inventory.unavailable && <p role="alert">Environment inventory is unavailable. Existing selections are retained.</p>}{inventory.keysUnavailable && <p role="alert">Protected key names could not be verified. Unreported names may exist; values are never fetched.</p>}
    {inventory.more && <button type="button" className="hud-btn" onClick={inventory.loadMore}>Load more environments</button>}
    {servicesUnavailable && <p>Recorded service inventory is incomplete. Existing binding names are retained; review the source for current service names.</p>}
    {invalidServices.length > 0 && <><p role="alert">Dockerfile apps support only the deploy runtime component. Saved binding names {invalidServices.map((name) => <code key={name}> {name} </code>)} are retained until you explicitly replace them.</p>{canEdit && <ConfirmButton className="hud-btn" disabled={busy} confirmLabel="Replace these binding names with deploy only?" onConfirm={() => change({ scope: 'service', bindings: { deploy: value.bindings.deploy ?? [] } })}>Use deploy runtime bindings</ConfirmButton>}</>}
    <ServiceEnvironmentSelector services={services} environment={inventory.selected} scope={value.scope} bindings={value.bindings} onChange={(scope, bindings) => change({ scope, bindings })} disabled={!canEdit || busy || invalidServices.length > 0} allowNamedService={!dockerfile} deploymentKind={dockerfile ? 'dockerfile' : 'compose'} />
    {!value.environmentId && value.scope === 'service' && Object.values(value.bindings).some((keys) => keys.length) && <p role="alert">Select an environment before deploying these key bindings.</p>}
    {error && <p role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}{stale && <p role="alert">This app changed during editing. Your selections are retained. Compare its latest version before reapplying.</p>}
    {canEdit && draft && <Actions><button type="button" className="hud-btn hud-btn-primary" disabled={busy || stale || invalidServices.length > 0 || draft.base.version === undefined} onClick={() => void save()}>Save service bindings</button><ConfirmButton className="hud-btn" disabled={busy} confirmLabel="Discard these service bindings?" onConfirm={() => { drafts.clearDraft(owner, key); setError(null); }}>Discard binding draft</ConfirmButton>{stale && <ConfirmButton className="hud-btn" disabled={busy} confirmLabel="Apply these bindings against the latest app version?" onConfirm={() => change({ base: app })}>Use latest app version</ConfirmButton>}</Actions>}
    <p className="workspace-note">Bindings stay in session memory during navigation and theme changes. <Link to={`/environments${value.environmentId ? `?environment=${encodeURIComponent(value.environmentId)}` : ''}`}>Manage configuration and protected values ↗</Link></p>
  </>;
}
