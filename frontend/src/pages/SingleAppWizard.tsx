import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import { useAuth } from '../auth/OIDCProvider';
import type { AppDeployTarget, TargetCapability } from '../types/app';
import Airlock from '../components/airlock/Airlock';
import ServiceEnvironmentSelector from '../components/environment/ServiceEnvironmentSelector';
import { useDraftMutationFence } from '../components/environment/useDraftMutationFence';
import { useEnvironmentInventory } from '../components/environment/useEnvironmentInventory';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Check, Field, FormError, Select, TextInput } from '../components/ui/form';
import { useConfigurationDraftStore } from '../stores/configurationDraftStore';
import { pushToast } from '../stores/toastStore';
import { configurationError } from '../utils/environmentEditing';

interface SingleAppDraft {
  name: string; repo: string; branch: string; path: string; prefix: string;
  kind: AppDeployTarget['kind']; namespace: string; registry: string;
  environmentId: string; runtimeKeys: string[]; autoDeploy: boolean;
}
const draftKey = 'new-app:dockerfile';
const emptyDraft: SingleAppDraft = { name: '', repo: '', branch: 'main', path: 'Dockerfile', prefix: '', kind: 'docker-host', namespace: 'default', registry: '', environmentId: '', runtimeKeys: [], autoDeploy: false };

/** New Dockerfile apps opt into an explicit runtime subset, including none. */
export default function SingleAppWizard() {
  const navigate = useNavigate();
  const { user } = useAuth();
  const owner = user?.sub ?? '';
  const canEdit = !!user?.roles.some((role) => role === 'admin' || role === 'operator');
  const drafts = useConfigurationDraftStore();
  const currentMutation = useDraftMutationFence();
  const draft = (drafts.owner === owner ? drafts.drafts[draftKey] : undefined) as SingleAppDraft | undefined;
  const value = draft ?? emptyDraft;
  const inventory = useEnvironmentInventory(value.environmentId);
  const [targets, setTargets] = useState<TargetCapability[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => { setBusy(false); setError(null); }, [currentMutation]);
  useEffect(() => {
    let active = true;
    const generation = useConfigurationDraftStore.getState().generation;
    void appsApi.deploymentCapabilities().then((result) => { if (active && currentMutation(generation)) setTargets(result.targets); }).catch(() => { if (active && currentMutation(generation)) setError('Could not load deployment targets.'); });
    return () => { active = false; };
  }, [currentMutation]);
  const change = (patch: Partial<SingleAppDraft>) => { if (canEdit && !busy) drafts.setDraft(owner, draftKey, { ...value, ...patch }); };
  const submit = async () => {
    if (!canEdit || busy || !draft) return;
    const generation = drafts.generation;
    setBusy(true); setError(null);
    try {
      const app = await appsApi.create({
        name: draft.name.trim(), githubRepo: draft.repo.trim(), branch: draft.branch.trim() || 'main',
        buildPlan: { kind: 'dockerfile', path: draft.path, environmentScope: 'service', serviceEnvironment: { deploy: draft.runtimeKeys } },
        deployTarget: { kind: draft.kind, prefix: draft.prefix, namespace: draft.kind === 'kubernetes' ? draft.namespace : undefined },
        registryRef: draft.registry, environmentId: draft.environmentId, autoDeploy: draft.autoDeploy,
      });
      drafts.clearDraftIf(owner, draftKey, draft, generation);
      if (!currentMutation(generation)) return;
      pushToast('success', `${app.name} saved.`);
      navigate(`/apps/${app.id}`);
    } catch (failure) { if (currentMutation(generation)) setError(configurationError(failure)); }
    finally { if (currentMutation(generation)) setBusy(false); }
  };
  const missingEnvironment = !value.environmentId && value.runtimeKeys.length > 0;
  return <Airlock wide title="New Dockerfile app" seed={11}>
    <p>Build one image from a public repository. For private GitHub access, service ports, database bindings and a deployment preview, <Link to="/apps/new">import a Compose file</Link>.</p>
    {!canEdit && <p role="status">An administrator or operator can save a new app. You can inspect this form.</p>}
    <fieldset disabled={busy || !canEdit} className="deployment-fields">
      <div className="panel-grid">
        <Field label="App name"><TextInput value={value.name} onChange={(event) => change({ name: event.target.value })} autoFocus /></Field>
        <Field label="Deployment prefix"><TextInput value={value.prefix} onChange={(event) => change({ prefix: event.target.value })} placeholder="shop-production" /></Field>
        <Field label="GitHub repository"><TextInput value={value.repo} onChange={(event) => change({ repo: event.target.value })} placeholder="owner/repo" /></Field>
        <Field label="Branch or tag"><TextInput value={value.branch} onChange={(event) => change({ branch: event.target.value })} /></Field>
        <Field label="Dockerfile path" hint="Relative to the repository root."><TextInput value={value.path} onChange={(event) => change({ path: event.target.value })} /></Field>
        <Field label="Deployment target"><Select value={value.kind} onChange={(event) => change({ kind: event.target.value as AppDeployTarget['kind'] })} options={targets.filter((target) => target.available && (target.kind === 'docker-host' || target.kind === 'kubernetes')).map((target) => ({ value: target.kind, label: target.kind === 'docker-host' ? 'Docker on Cooker host' : 'Kubernetes' }))} /></Field>
        {value.kind === 'kubernetes' && <Field label="Namespace"><TextInput value={value.namespace} onChange={(event) => change({ namespace: event.target.value })} /></Field>}
        <Field label="Image registry" hint="Leave empty for the configured default."><TextInput value={value.registry} onChange={(event) => change({ registry: event.target.value })} /></Field>
        <Field label="Runtime environment"><Select value={value.environmentId} onChange={(event) => change({ environmentId: event.target.value })} options={[{ value: '', label: 'No linked environment' }, ...(!inventory.environments.some((environment) => environment.id === value.environmentId) && value.environmentId ? [{ value: value.environmentId, label: `${value.environmentId} · not in loaded inventory` }] : []), ...inventory.environments.map((environment) => ({ value: environment.id, label: environment.name }))]} /></Field>
      </div>
      {inventory.unavailable && <p role="alert">Environment inventory is unavailable. Your selections are retained.</p>}
      {inventory.keysUnavailable && <p role="alert">Protected key names could not be verified. Unreported names may exist; values are never fetched.</p>}
      {inventory.more && <button type="button" className="hud-btn" onClick={inventory.loadMore}>Load more environments</button>}
      <ServiceEnvironmentSelector services={['deploy']} environment={inventory.selected} scope="service" bindings={{ deploy: value.runtimeKeys }} onChange={(_scope, bindings) => change({ runtimeKeys: bindings.deploy ?? [] })} disabled={busy || !canEdit} deploymentKind="dockerfile" />
      {missingEnvironment && <p role="alert">Select an environment before saving these runtime key bindings.</p>}
      <p>Docker runs this image without published ports. Use Compose to choose service ports.</p>
      <Check label="Deploy automatically on a matching GitHub push" checked={value.autoDeploy} onChange={(autoDeploy) => change({ autoDeploy })} />
    </fieldset>
    <FormError>{error}</FormError>
    <p className="workspace-note">This draft stays in session memory during navigation and theme changes. Saving the app does not deploy it.</p>
    <div className="wizard-nav"><Link to="/apps" className="hud-btn">Cancel</Link>{canEdit && draft && <ConfirmButton className="hud-btn" disabled={busy} confirmLabel="Discard this Dockerfile app draft?" onConfirm={() => { drafts.clearDraft(owner, draftKey); setError(null); }}>Discard app draft</ConfirmButton>}<span className="spacer" /><button type="button" className="hud-btn hud-btn-primary" disabled={!canEdit || busy || missingEnvironment || !value.name.trim() || !value.repo.trim() || !/^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$/.test(value.prefix) || !targets.some((target) => target.kind === value.kind && target.available)} onClick={() => void submit()}>{busy ? 'Saving…' : 'Save app'}</button></div>
  </Airlock>;
}
