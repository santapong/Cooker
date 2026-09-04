import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import { hostsApi } from '../api/hosts';
import { environmentsApi } from '../api/environments';
import { settingsApi } from '../api/settings';
import type { AppBuildPlan, AppDeployTarget } from '../types/app';
import type { Host, ClusterConfig } from '../types/infra';
import type { Environment } from '../types/environment';
import Airlock from '../components/airlock/Airlock';
import Caps from '../components/ui/Caps';
import { Actions, Check, Field, FormError, Select, TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';

const STEPS = ['Source', 'Build', 'Run'] as const;
type Recipe = 'go' | 'node-static' | 'worker';
const RECIPES: { id: Recipe; label: string; sub: string }[] = [
  { id: 'go', label: 'Go service', sub: 'HTTP service, single binary' },
  { id: 'node-static', label: 'Static site', sub: 'Built assets behind a web server' },
  { id: 'worker', label: 'Worker', sub: 'Long-running process, no ingress' },
];
const PLANS: { id: AppBuildPlan['kind']; label: string; sub: string }[] = [
  { id: 'dockerfile', label: 'Dockerfile', sub: 'Build from the repository Dockerfile' },
  { id: 'compose', label: 'Compose', sub: 'Multi-service docker-compose.yml' },
  { id: 'buildpack', label: 'Buildpack', sub: 'Detect and build without a Dockerfile' },
];
const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** New App — a three-step airlock: source, build, run (spec §5.D). */
export default function NewAppWizard() {
  const navigate = useNavigate();
  const [step, setStep] = useState(0);
  const [name, setName] = useState('');
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('main');
  const [planKind, setPlanKind] = useState<AppBuildPlan['kind']>('dockerfile');
  const [planPath, setPlanPath] = useState('');
  const [recipe, setRecipe] = useState<Recipe>('go');
  const [detecting, setDetecting] = useState(false);
  const [detected, setDetected] = useState<string | null>(null);
  const [targetKind, setTargetKind] = useState<AppDeployTarget['kind']>('docker-host');
  const [hostId, setHostId] = useState('');
  const [namespace, setNamespace] = useState('default');
  const [region, setRegion] = useState('');
  const [service, setService] = useState('');
  const [environmentId, setEnvironmentId] = useState('');
  const [autoDeploy, setAutoDeploy] = useState(false);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [clusters, setClusters] = useState<ClusterConfig[]>([]);
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    hostsApi.list({ limit: 100 }).then((h) => setHosts((h ?? []).filter((x) => x.kind !== 'kubernetes'))).catch(() => {});
    settingsApi.listClusters().then((c) => setClusters(c ?? [])).catch(() => {});
    environmentsApi.list({ limit: 100 }).then((e) => setEnvs([...(e ?? [])].sort((a, b) => a.order - b.order))).catch(() => {});
  }, []);

  const leaveSource = async () => {
    setError(null);
    if (!name.trim() || !repo.trim()) {
      setError('Name and repository are required.');
      return;
    }
    setStep(1);
    if (detected !== null) return;
    setDetecting(true);
    try {
      const res = await appsApi.detectBuild(repo.trim(), branch.trim() || 'main');
      setDetected(`${res.plan.kind}${res.plan.path ? ` at ${res.plan.path}` : ''} · ${res.suggestedRecipe}`);
      setPlanKind(res.plan.kind as AppBuildPlan['kind']);
      if (res.plan.path) setPlanPath(res.plan.path);
      setRecipe(res.suggestedRecipe);
    } catch {
      setDetected('Could not inspect the repository — choose the build manually.');
    } finally {
      setDetecting(false);
    }
  };

  const submit = async () => {
    setBusy(true);
    setError(null);
    const deployTarget: AppDeployTarget =
      targetKind === 'docker-host'
        ? { kind: 'docker-host', hostId: hostId || undefined }
        : targetKind === 'kubernetes'
          ? { kind: 'kubernetes', namespace: namespace || 'default' }
          : { kind: 'cloud-run', region: region || undefined, service: service || undefined };
    try {
      const app = await appsApi.create({
        name: name.trim(),
        githubRepo: repo.trim(),
        branch: branch.trim() || 'main',
        buildPlan: { kind: planKind, path: planPath || undefined },
        deployTarget,
        environmentId: environmentId || undefined,
        autoDeploy,
      });
      pushToast('success', `App "${app.name}" created.`);
      navigate(`/apps/${app.id}`);
    } catch (e) {
      setError(message(e));
      setBusy(false);
    }
  };

  return (
    <Airlock wide title="New app" seed={11}>
      <div className="wizard-steps" aria-label="Steps">
        {STEPS.map((s, i) => (
          <span key={s} className={`wizard-step${i < step ? ' is-done' : ''}${i === step ? ' is-current' : ''}`} aria-current={i === step ? 'step' : undefined}>
            <Caps>
              {i + 1} · {s}
            </Caps>
          </span>
        ))}
      </div>

      {step === 0 && (
        <div className="airlock-step" key="source">
          <p>Point Cooker at a repository. It inspects the branch and suggests how to build it.</p>
          <Field label="App name">
            <TextInput value={name} onChange={(e) => setName(e.target.value)} placeholder="web" autoFocus />
          </Field>
          <div className="panel-grid">
            <Field label="GitHub repository">
              <TextInput value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="owner/repo" />
            </Field>
            <Field label="Branch">
              <TextInput value={branch} onChange={(e) => setBranch(e.target.value)} placeholder="main" />
            </Field>
          </div>
        </div>
      )}

      {step === 1 && (
        <div className="airlock-step" key="build">
          <p>{detecting ? 'Inspecting the repository…' : (detected ?? 'How should this app be built?')}</p>
          <Caps>Build</Caps>
          <div className="option-grid" role="group" aria-label="Build plan">
            {PLANS.map((p) => (
              <button key={p.id} type="button" className="option" aria-pressed={planKind === p.id} onClick={() => setPlanKind(p.id)}>
                <Caps>{p.label}</Caps>
                <small>{p.sub}</small>
              </button>
            ))}
          </div>
          <Field label={planKind === 'compose' ? 'Compose file' : 'Path'} hint="Relative to the repository root; leave empty for the default.">
            <TextInput value={planPath} onChange={(e) => setPlanPath(e.target.value)} placeholder={planKind === 'compose' ? 'docker-compose.yml' : 'Dockerfile'} />
          </Field>
          <Caps>Recipe</Caps>
          <div className="option-grid" role="group" aria-label="Recipe">
            {RECIPES.map((r) => (
              <button key={r.id} type="button" className="option" aria-pressed={recipe === r.id} onClick={() => setRecipe(r.id)}>
                <Caps>{r.label}</Caps>
                <small>{r.sub}</small>
              </button>
            ))}
          </div>
        </div>
      )}

      {step === 2 && (
        <div className="airlock-step" key="run">
          <p>Where should it run, and should every push deploy it?</p>
          <div className="option-grid" role="group" aria-label="Deploy target">
            {(
              [
                ['docker-host', 'Docker host', 'A daemon Cooker can reach'],
                ['kubernetes', 'Kubernetes', 'A namespace in a configured cluster'],
                ['cloud-run', 'Cloud Run', 'Google Cloud Run service'],
              ] as [AppDeployTarget['kind'], string, string][]
            ).map(([kind, label, sub]) => (
              <button key={kind} type="button" className="option" aria-pressed={targetKind === kind} onClick={() => setTargetKind(kind)}>
                <Caps>{label}</Caps>
                <small>{sub}</small>
              </button>
            ))}
          </div>
          <div className="panel-grid">
            {targetKind === 'docker-host' && (
              <Field label="Host" hint={hosts.length ? undefined : 'No docker hosts yet — the local daemon is used.'}>
                <Select value={hostId} onChange={(e) => setHostId(e.target.value)} options={[{ value: '', label: 'local daemon' }, ...hosts.map((h) => ({ value: h.id, label: `${h.name} · ${h.kind}` }))]} />
              </Field>
            )}
            {targetKind === 'kubernetes' && (
              <Field label="Namespace" hint={clusters.length ? `${clusters.length} cluster${clusters.length === 1 ? '' : 's'} configured` : 'No clusters configured in Settings yet.'}>
                <TextInput value={namespace} onChange={(e) => setNamespace(e.target.value)} placeholder="default" />
              </Field>
            )}
            {targetKind === 'cloud-run' && (
              <>
                <Field label="Region">
                  <TextInput value={region} onChange={(e) => setRegion(e.target.value)} placeholder="europe-west1" />
                </Field>
                <Field label="Service">
                  <TextInput value={service} onChange={(e) => setService(e.target.value)} placeholder="web" />
                </Field>
              </>
            )}
            <Field label="Environment" hint="Optional — injects its variables and secrets.">
              <Select value={environmentId} onChange={(e) => setEnvironmentId(e.target.value)} options={[{ value: '', label: 'none' }, ...envs.map((env) => ({ value: env.id, label: env.name }))]} />
            </Field>
          </div>
          <Check label="Deploy automatically on every push (webhook)" checked={autoDeploy} onChange={setAutoDeploy} />
          <div className="kv">
            <Caps>Summary</Caps>
            <span className="v">
              {name} · {repo}@{branch || 'main'} · {planKind} · {recipe} · {targetKind}
            </span>
          </div>
        </div>
      )}

      <FormError>{error}</FormError>
      <div className="wizard-nav">
        <button type="button" className="hud-btn" onClick={() => setStep((s) => Math.max(0, s - 1))} disabled={step === 0 || busy}>
          Back
        </button>
        <span className="spacer" />
        <span className="mono" style={{ fontSize: 12, color: 'var(--ink-3)' }}>
          step {step + 1} / {STEPS.length}
        </span>
        {step === 0 && (
          <button type="button" className="hud-btn hud-btn-primary" onClick={leaveSource}>
            Next
          </button>
        )}
        {step === 1 && (
          <button type="button" className="hud-btn hud-btn-primary" onClick={() => setStep(2)}>
            Next
          </button>
        )}
        {step === 2 && (
          <Actions>
            <button type="button" className="hud-btn hud-btn-primary" onClick={submit} disabled={busy}>
              {busy ? 'Creating…' : 'Create app'}
            </button>
          </Actions>
        )}
      </div>
    </Airlock>
  );
}
