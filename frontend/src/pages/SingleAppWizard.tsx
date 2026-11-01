import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import { environmentsApi } from '../api/environments';
import type { AppDeployTarget, TargetCapability } from '../types/app';
import type { Environment } from '../types/environment';
import Airlock from '../components/airlock/Airlock';
import { Check, Field, FormError, Select, TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';

/** The existing single-image path. Compose imports use the reviewed wizard. */
export default function SingleAppWizard() {
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('main');
  const [path, setPath] = useState('Dockerfile');
  const [prefix, setPrefix] = useState('');
  const [kind, setKind] = useState<AppDeployTarget['kind']>('docker-host');
  const [namespace, setNamespace] = useState('default');
  const [registry, setRegistry] = useState('');
  const [environmentId, setEnvironmentId] = useState('');
  const [autoDeploy, setAutoDeploy] = useState(false);
  const [targets, setTargets] = useState<TargetCapability[]>([]);
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let alive = true;
    void appsApi.deploymentCapabilities().then((r) => { if (alive) setTargets(r.targets); }).catch(() => { if (alive) setError('Could not load deployment targets.'); });
    void environmentsApi.list({ limit: 100 }).then((r) => { if (alive) setEnvs(r ?? []); }).catch(() => {});
    return () => { alive = false; };
  }, []);
  const submit = async () => {
    setBusy(true); setError(null);
    try {
      const app = await appsApi.create({
        name: name.trim(), githubRepo: repo.trim(), branch: branch.trim() || 'main',
        buildPlan: { kind: 'dockerfile', path },
        deployTarget: { kind, prefix, namespace: kind === 'kubernetes' ? namespace : undefined },
        registryRef: registry, environmentId, autoDeploy,
      });
      pushToast('success', `${app.name} saved.`);
      navigate(`/apps/${app.id}`);
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); setBusy(false); }
  };
  return <Airlock wide title="New Dockerfile app" seed={11}>
    <p>Build one image from a public repository. For private GitHub access, service ports, database bindings and a deployment preview, <Link to="/apps/new">import a Compose file</Link>.</p>
    <fieldset disabled={busy} className="deployment-fields">
      <div className="panel-grid">
        <Field label="App name"><TextInput value={name} onChange={(e) => setName(e.target.value)} autoFocus /></Field>
        <Field label="Deployment prefix"><TextInput value={prefix} onChange={(e) => setPrefix(e.target.value)} placeholder="shop-production" /></Field>
        <Field label="GitHub repository"><TextInput value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="owner/repo" /></Field>
        <Field label="Branch or tag"><TextInput value={branch} onChange={(e) => setBranch(e.target.value)} /></Field>
        <Field label="Dockerfile path" hint="Relative to the repository root."><TextInput value={path} onChange={(e) => setPath(e.target.value)} /></Field>
        <Field label="Deployment target"><Select value={kind} onChange={(e) => setKind(e.target.value as AppDeployTarget['kind'])} options={targets.filter((t) => t.available && (t.kind === 'docker-host' || t.kind === 'kubernetes')).map((t) => ({ value: t.kind, label: t.kind === 'docker-host' ? 'Docker on Cooker host' : 'Kubernetes' }))} /></Field>
        {kind === 'kubernetes' && <Field label="Namespace"><TextInput value={namespace} onChange={(e) => setNamespace(e.target.value)} /></Field>}
        <Field label="Image registry" hint="Leave empty for the configured default."><TextInput value={registry} onChange={(e) => setRegistry(e.target.value)} /></Field>
        <Field label="Runtime environment"><Select value={environmentId} onChange={(e) => setEnvironmentId(e.target.value)} options={[{ value: '', label: 'No linked environment' }, ...envs.filter((e) => !e.id.startsWith('_')).map((e) => ({ value: e.id, label: e.name }))]} /></Field>
      </div>
      <p>Docker runs this image without published ports. The Kubernetes shortcut exposes port 80. Use Compose to choose service ports.</p>
      <Check label="Deploy automatically on a matching GitHub push" checked={autoDeploy} onChange={setAutoDeploy} />
    </fieldset>
    <FormError>{error}</FormError>
    <div className="wizard-nav"><Link to="/apps" className="hud-btn">Cancel</Link><span className="spacer" /><button type="button" className="hud-btn hud-btn-primary" disabled={busy || !name.trim() || !repo.trim() || !/^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$/.test(prefix) || !targets.some((t) => t.kind === kind && t.available)} onClick={() => void submit()}>{busy ? 'Saving…' : 'Save app'}</button></div>
  </Airlock>;
}
