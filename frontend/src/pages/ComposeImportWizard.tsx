import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { appsApi } from '../api/apps';
import { environmentsApi } from '../api/environments';
import type { AppBuildPlan, AppDeployTarget, AppModel, GitHubConnection, GitHubRepository, RepositoryInspection, TargetCapability } from '../types/app';
import type { Environment } from '../types/environment';
import Airlock from '../components/airlock/Airlock';
import Caps from '../components/ui/Caps';
import { Field, FormError, Select, TextArea, TextInput } from '../components/ui/form';
import StackPreview from '../components/apps/StackPreview';
import ExternalBindings from '../components/apps/ExternalBindings';
import { pushToast } from '../stores/toastStore';
import '../components/apps/deployment-wizard.css';

const STEPS = ['Repository', 'Compose files', 'Preview', 'Destination', 'Review'];
const TARGET_NAMES: Record<AppDeployTarget['kind'], string> = { 'docker-host': 'Docker on Cooker host', kubernetes: 'Kubernetes', ecs: 'AWS ECS · Fargate', 'cloud-run': 'Google Cloud Run' };
const message = (e: unknown) => e instanceof Error ? e.message : String(e);
const suggestedPrefix = (name: string) => name.toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40).replace(/-+$/, '');
type Draft = Partial<AppModel> & { name: string; githubRepo: string; branch: string; buildPlan: AppBuildPlan; deployTarget: AppDeployTarget };
const emptyDraft = (): Draft => ({ name: '', githubRepo: '', branch: 'main', buildPlan: { kind: 'compose', files: [] }, deployTarget: { kind: 'docker-host', prefix: '', externalServices: [] }, autoDeploy: false, canary: { strategy: 'rolling' } });

export default function ComposeImportWizard() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const appId = params.get('appId');
  const [step, setStep] = useState(0);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [connection, setConnection] = useState<GitHubConnection | null>(null);
  const [sourceError, setSourceError] = useState<string | null>(null);
  const [repos, setRepos] = useState<GitHubRepository[]>([]);
  const [repoPage, setRepoPage] = useState(1);
  const [moreRepos, setMoreRepos] = useState(false);
  const [repoBusy, setRepoBusy] = useState(false);
  const [repoQuery, setRepoQuery] = useState('');
  const [revisions, setRevisions] = useState<string[]>([]);
  const [candidates, setCandidates] = useState<RepositoryInspection['candidates']>([]);
  const [fileQuery, setFileQuery] = useState('');
  const [customPath, setCustomPath] = useState('');
  const [preview, setPreview] = useState<RepositoryInspection | null>(null);
  const [previewGeneration, setPreviewGeneration] = useState(0);
  const [reviewed, setReviewed] = useState<string | null>(null);
  const [targets, setTargets] = useState<TargetCapability[]>([]);
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [variables, setVariables] = useState('');
  const [profiles, setProfiles] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const request = useRef(0);
  const repoRequest = useRef(0);
  const revisionRequest = useRef(0);
  const titleRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    titleRef.current?.focus({ preventScroll: true });
    titleRef.current?.scrollIntoView({ block: 'start' });
  }, [step]);

  useEffect(() => {
    let alive = true;
    const tickets = [request, repoRequest, revisionRequest];
    void appsApi.githubConnection().then((c) => { if (alive) setConnection(c); }).catch((e) => { if (alive) setSourceError(message(e)); });
    void appsApi.deploymentCapabilities().then((c) => { if (alive) setTargets(c.targets); }).catch((e) => { if (alive) setError(message(e)); });
    void environmentsApi.list({ limit: 100 }).then((e) => { if (alive) setEnvs(e ?? []); }).catch((e) => { if (alive) setError(message(e)); });
    if (appId) void appsApi.get(appId).then((app) => {
      if (!alive) return;
      const buildPlan = { ...app.buildPlan, kind: 'compose' as const, files: app.buildPlan?.files ?? (app.buildPlan?.path ? [app.buildPlan.path] : []) };
      setDraft({ ...app, autoDeploy: false, buildPlan, deployTarget: { ...app.deployTarget, prefix: app.deployTarget.prefix || suggestedPrefix(app.name) } });
      setVariables(Object.entries(buildPlan.variables ?? {}).map(([k, v]) => `${k}=${v}`).join('\n'));
      setProfiles(buildPlan.profiles?.join(', ') ?? '');
    }).catch((e) => { if (alive) setError(message(e)); });
    return () => { alive = false; tickets.forEach((ticket) => ticket.current++); };
  }, [appId]);

  const change = (patch: Partial<Draft>) => { request.current++; setDraft((d) => ({ ...d, ...patch })); setReviewed(null); setError(null); };
  const plan = (patch: Partial<AppBuildPlan>) => change({ buildPlan: { ...draft.buildPlan, ...patch } });
  const target = (patch: Partial<AppDeployTarget>) => change({ deployTarget: { ...draft.deployTarget, ...patch } });
  const source = (repo: string, branch: string, installationId = draft.buildPlan.installationId) => {
    change({ githubRepo: repo, branch, buildPlan: { kind: 'compose', files: [], installationId }, deployTarget: { ...draft.deployTarget, externalServices: [] } });
    setCandidates([]); setPreview(null); setRevisions([]); setProfiles(''); setVariables(''); revisionRequest.current++;
  };
  const loadRepos = async (id: number, page = 1) => {
    const seq = ++repoRequest.current;
    setRepoBusy(true); setSourceError(null);
    try {
      const res = await appsApi.githubRepositories(id, page);
      if (seq !== repoRequest.current) return;
      setRepos((items) => page === 1 ? res.repositories : [...items, ...res.repositories]); setMoreRepos(res.hasMore); setRepoPage(page);
    } catch (e) { if (seq === repoRequest.current) setSourceError(message(e)); }
    finally { if (seq === repoRequest.current) setRepoBusy(false); }
  };
  const chooseRepo = (repo: string) => {
    const found = repos.find((r) => r.full_name === repo);
    source(repo, found?.default_branch || 'main');
    if (!draft.name) setDraft((d) => ({ ...d, name: repo.split('/')[1] || '', deployTarget: { ...d.deployTarget, prefix: suggestedPrefix(repo.split('/')[1] || '') } }));
    const seq = ++revisionRequest.current;
    void appsApi.githubRevisions(repo, draft.buildPlan.installationId).then((r) => { if (seq === revisionRequest.current) setRevisions(r.revisions.map((v) => v.name)); }).catch((e) => { if (seq === revisionRequest.current) setSourceError(message(e)); });
  };
  const inspect = async (app: Draft, nextStep: number, discover = false) => {
    const seq = ++request.current;
    setBusy(true); setError(null);
    try {
      const res = await appsApi.inspect(app);
      if (seq !== request.current) return;
      const updated = { ...app, buildPlan: { ...app.buildPlan, commit: res.commit } };
      setCandidates(res.candidates);
      if (discover && !(updated.buildPlan.files?.length)) {
        const first = res.candidates.find((c) => /^(compose|docker-compose)\.ya?ml$/.test(c.path)) ?? res.candidates[0];
        if (first) updated.buildPlan = { ...updated.buildPlan, files: [first.path], path: first.path };
      }
      setDraft(updated);
      if (!discover) { setPreview(res); setPreviewGeneration((n) => n + 1); setReviewed(res.deployable ? JSON.stringify(updated) : null); }
      setStep(nextStep);
    } catch (e) { if (seq === request.current) setError(message(e)); }
    finally { if (seq === request.current) setBusy(false); }
  };
  const withVariables = (): Draft => {
    const pairs = variables.split('\n').map((line) => line.trim()).filter(Boolean).map((line) => {
      const index = line.indexOf('='); if (index < 1) throw new Error('Use KEY=value for each Compose interpolation input.');
      return [line.slice(0, index).trim(), line.slice(index + 1)];
    });
    return { ...draft, buildPlan: { ...draft.buildPlan, variables: Object.fromEntries(pairs), profiles: profiles.split(',').map((p) => p.trim()).filter(Boolean) } };
  };
  const next = async () => {
    try {
      if (step === 0) {
        if (!draft.name.trim() || !/^[^/\s]+\/[^/\s]+$/.test(draft.githubRepo.trim())) throw new Error('Enter an app name and a GitHub repository in owner/repo format.');
        await inspect({ ...draft, name: draft.name.trim(), githubRepo: draft.githubRepo.trim(), branch: draft.branch.trim() || 'main', deployTarget: { ...draft.deployTarget, prefix: draft.deployTarget.prefix || suggestedPrefix(draft.name) } }, 1, true);
      } else if (step === 1) {
        if (!draft.buildPlan.files?.length) throw new Error('Select a Compose file or enter its repository path.');
        await inspect(withVariables(), 2);
      } else if (step === 2) setStep(3);
      else if (step === 3) {
        if (!/^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$/.test(draft.deployTarget.prefix || '')) throw new Error('Use 1–40 lowercase letters, digits or hyphens, starting and ending with a letter or digit.');
        await inspect(withVariables(), 4);
      }
    } catch (e) { setError(message(e)); }
  };
  const save = async () => {
    if (!preview?.deployable || reviewed !== JSON.stringify(draft)) { setError('Refresh the deployment review before saving.'); return; }
    setBusy(true); setError(null);
    try {
      const saved = appId ? await appsApi.update(appId, draft as AppModel) : await appsApi.create(draft);
      pushToast('success', `${saved.name} saved with reviewed commit ${draft.buildPlan.commit?.slice(0, 8)}.`);
      navigate(`/apps/${saved.id}`);
    } catch (e) { setError(message(e)); setBusy(false); }
  };
  const files = draft.buildPlan.files ?? [];
  const setFiles = (files: string[]) => plan({ files, path: files[0] });
  const moveFile = (index: number, delta: number) => { const list = [...files]; [list[index], list[index + delta]] = [list[index + delta], list[index]]; setFiles(list); };

  return <div className="deployment-wizard"><Airlock wide title={appId ? 'Review app deployment' : 'New app'} titleRef={titleRef} seed={11}>
    <div className="wizard-steps" aria-label="Steps">{STEPS.map((label, i) => <span key={label} className={`wizard-step${step === i ? ' is-current' : i < step ? ' is-done' : ''}`} aria-current={step === i ? 'step' : undefined}><Caps>{i + 1} · {label}</Caps></span>)}</div>
    <fieldset className="deployment-fields" disabled={busy}>
      {step === 0 && <div className="airlock-step">
        <p>Choose your source. Cooker will find Compose files and show the deployment graph before anything runs.</p>
        <div className="source-actions">
          {connection?.installUrl && <a className="hud-btn" href={connection.installUrl} target="_blank" rel="noreferrer">Connect GitHub</a>}
          <button type="button" className="hud-btn" onClick={() => { setSourceError(null); void appsApi.githubConnection().then(setConnection).catch((e) => setSourceError(message(e))); }}>Refresh connection</button>
          {!appId && <Link to="/apps/new?mode=dockerfile">Use a single Dockerfile</Link>}
        </div>
        {connection && !connection.configured && <p className="source-note">GitHub App access needs administrator setup. You can inspect a public repository now.</p>}
        {connection?.configured && <>
          <Field label="GitHub account"><Select value={draft.buildPlan.installationId || ''} options={[{ value: '', label: 'Public repository · enter manually' }, ...connection.installations.map((i) => ({ value: String(i.id), label: i.account.login }))]} onChange={(e) => {
            const id = Number(e.target.value) || 0; source('', 'main', id); setRepos([]); setMoreRepos(false); repoRequest.current++; setRepoBusy(false); if (id) void loadRepos(id);
          }} /></Field>
          {connection.installations.length === 0 && <p className="source-note">Install the GitHub App, then have an administrator enable that installation for this workspace.</p>}
        </>}
        {!!draft.buildPlan.installationId && <>
          {repos.length === 0 && !repoBusy && <button type="button" className="hud-btn" onClick={() => void loadRepos(draft.buildPlan.installationId!)}>Load connected repositories</button>}
          <Field label="Find a repository"><TextInput value={repoQuery} onChange={(e) => setRepoQuery(e.target.value)} placeholder="Search connected repositories" /></Field>
          <Field label="Connected repository"><Select value={draft.githubRepo} options={[{ value: '', label: repoBusy ? 'Loading repositories…' : 'Select a repository' }, ...(!repos.some((r) => r.full_name === draft.githubRepo) && draft.githubRepo ? [{ value: draft.githubRepo, label: `${draft.githubRepo} · Current source` }] : []), ...repos.filter((r) => r.full_name.toLowerCase().includes(repoQuery.toLowerCase())).map((r) => ({ value: r.full_name, label: `${r.full_name}${r.private ? ' · Private' : ''}` }))]} onChange={(e) => chooseRepo(e.target.value)} /></Field>
          {moreRepos && <button type="button" className="hud-btn" disabled={repoBusy} onClick={() => void loadRepos(draft.buildPlan.installationId!, repoPage + 1)}>Load more repositories</button>}
        </>}
        {sourceError && <FormError>{sourceError}</FormError>}
        <div className="panel-grid">
          <Field label="App name"><TextInput value={draft.name} onChange={(e) => change({ name: e.target.value })} placeholder="shop" autoFocus /></Field>
          <Field label="GitHub repository"><TextInput value={draft.githubRepo} onChange={(e) => source(e.target.value, draft.branch)} placeholder="owner/repo" /></Field>
          <Field label="Branch or tag"><TextInput value={draft.branch} onChange={(e) => source(draft.githubRepo, e.target.value)} list="github-revisions" placeholder="main" /><datalist id="github-revisions">{revisions.map((name) => <option key={name} value={name} />)}</datalist></Field>
        </div>
        {draft.buildPlan.commit && <p className="source-revision">Saved commit <code>{draft.buildPlan.commit.slice(0, 12)}</code><button type="button" className="hud-btn" onClick={() => plan({ commit: undefined })}>Inspect latest branch revision</button></p>}
      </div>}
      {step === 1 && <div className="airlock-step">
        <p>Select the base Compose file first, then any overrides in the order they should apply.</p>
        <div className="source-revision">{draft.githubRepo} · <code>{draft.buildPlan.commit?.slice(0, 12)}</code></div>
        <Field label="Filter files by path or prefix"><TextInput value={fileQuery} onChange={(e) => setFileQuery(e.target.value)} placeholder="compose.prod or deploy/" /></Field>
        <div className="compose-candidates">{candidates.filter((c) => c.path.toLowerCase().includes(fileQuery.toLowerCase())).map((c) => <label className="compose-candidate" key={c.path}><input type="checkbox" checked={files.includes(c.path)} onChange={(e) => setFiles(e.target.checked ? [...files, c.path] : files.filter((p) => p !== c.path))} /><span><strong>{c.path}</strong><small>{c.error || c.services.join(' · ') || 'Override or empty service list'}</small></span></label>)}</div>
        {candidates.length === 0 && <p className="source-note">No Compose filenames were found. Enter a repository path below.</p>}
        <div className="panel-grid"><Field label="Custom Compose path"><TextInput value={customPath} onChange={(e) => setCustomPath(e.target.value)} placeholder="deploy/production.yaml" /></Field><button type="button" className="hud-btn" onClick={() => { const p = customPath.trim(); if (p && !files.includes(p)) setFiles([...files, p]); setCustomPath(''); }}>Add file</button></div>
        <ol className="compose-order" aria-label="Compose merge order">{files.map((path, i) => <li key={path}><code>{i + 1}. {path}{i === 0 ? ' · base' : ' · override'}</code><button type="button" className="hud-btn" disabled={i === 0} aria-label={`Move ${path} earlier`} onClick={() => moveFile(i, -1)}>↑</button><button type="button" className="hud-btn" disabled={i === files.length - 1} aria-label={`Move ${path} later`} onClick={() => moveFile(i, 1)}>↓</button><button type="button" className="hud-btn" aria-label={`Remove ${path}`} onClick={() => setFiles(files.filter((p) => p !== path))}>Remove</button></li>)}</ol>
        <Field label="Compose profiles" hint="Optional, separated by commas."><TextInput value={profiles} onChange={(e) => { setProfiles(e.target.value); setReviewed(null); }} placeholder="production, worker" /></Field>
        <EnvironmentInput value={draft.environmentId || ''} envs={envs} onChange={(environmentId) => change({ environmentId })} />
        <Field label="Compose interpolation inputs" hint="Optional KEY=value lines for non-secret settings. Put credentials in the selected environment."><TextArea rows={3} value={variables} onChange={(e) => { setVariables(e.target.value); setReviewed(null); }} placeholder="APP_PORT=8080" /></Field>
      </div>}
      {step === 2 && <div className="airlock-step"><p>Inspect the service map and Dockerfiles. Build and deployment have not started.</p>{preview && <StackPreview key={previewGeneration} preview={preview} />}</div>}
      {step === 3 && <div className="airlock-step">
        <Field label="Deployment prefix" hint="Scopes generated workload names, for example shop-production-api."><TextInput value={draft.deployTarget.prefix || ''} onChange={(e) => target({ prefix: e.target.value })} placeholder="shop-production" /></Field>
        <div className="option-grid" role="group" aria-label="Deploy target">{targets.map((cap) => <button key={cap.kind} type="button" className="option" aria-pressed={draft.deployTarget.kind === cap.kind} disabled={!cap.available} onClick={() => target({ kind: cap.kind, namespace: undefined, hostId: undefined, region: undefined, service: undefined })}><Caps>{TARGET_NAMES[cap.kind]}</Caps><small>{cap.available ? 'Configured target' : cap.reason || 'Administrator setup required'}</small></button>)}</div>
        {targets.length === 0 && <FormError>Deployment targets could not be loaded. Refresh the page before continuing.</FormError>}
        {draft.deployTarget.kind === 'kubernetes' && <Field label="Kubernetes namespace"><TextInput value={draft.deployTarget.namespace || ''} onChange={(e) => target({ namespace: e.target.value })} placeholder="default" /></Field>}
        {draft.deployTarget.kind === 'ecs' && <p className="source-note">Deploy containers to the configured ECS Fargate cluster. The cluster, network and IAM roles must already exist.</p>}
        <Field label="Image registry" hint="Leave empty to use Cooker's configured registry. Cloud targets must be able to pull these images."><TextInput value={draft.registryRef || ''} onChange={(e) => change({ registryRef: e.target.value })} placeholder="123456789012.dkr.ecr.region.amazonaws.com" /></Field>
        <EnvironmentInput value={draft.environmentId || ''} envs={envs} onChange={(environmentId) => change({ environmentId })} />
        <h2>Database connections</h2>
        <p className="source-note">Bind a database service to an existing resource and select which applications use it. Configure its connection keys in the environment and provide network access from the workload.</p>
        <ExternalBindings services={preview?.graph?.services ?? []} bindings={draft.deployTarget.externalServices ?? []} onChange={(externalServices) => target({ externalServices })} />
      </div>}
      {step === 4 && <div className="airlock-step">
        <div className="stack-review"><h3>{draft.name} · {draft.deployTarget.prefix}</h3><p>{draft.githubRepo} · {draft.branch} · <code>{draft.buildPlan.commit?.slice(0, 12)}</code></p><p>Files: {files.join(' → ')}</p>
          <table><thead><tr><th>Service</th><th>Target</th><th>Resource</th></tr></thead><tbody>{preview?.graph?.services.map((s) => { const binding = draft.deployTarget.externalServices?.find((b) => b.service === s.name); return <tr key={s.name}><td><span className="stack-mobile-label" aria-hidden="true">Service</span>{s.name}</td><td><span className="stack-mobile-label" aria-hidden="true">Target</span>{binding ? binding.provider === 'gcp-cloud-sql' ? 'GCP Cloud SQL' : 'External database' : TARGET_NAMES[draft.deployTarget.kind]}</td><td><span className="stack-mobile-label" aria-hidden="true">Resource</span>{binding?.resource || preview.workloads[s.name]}</td></tr>; })}</tbody></table>
          <p>Saving records this reviewed revision in Cooker. Use Deploy on the app page when you are ready to run it. Existing databases keep their data and lifecycle.</p>
          <p className="source-note">Reviewed Compose revisions use manual deployment. Inspect the latest branch revision here before deploying new source changes.</p>
        </div>
        {preview && <StackPreview key={previewGeneration} preview={preview} />}
      </div>}
    </fieldset>
    {error && <FormError>{error}</FormError>}
    <div className="wizard-nav">
      {step > 0 && <button type="button" className="hud-btn" disabled={busy} onClick={() => { setStep(step - 1); setError(null); }}>Back</button>}
      <Link to={appId ? `/apps/${appId}` : '/apps'} className="hud-btn hud-link">Cancel</Link><span className="spacer" />
      {step < 4 ? <button type="button" className="hud-btn hud-btn-primary" disabled={busy || step === 3 && !targets.some((t) => t.kind === draft.deployTarget.kind && t.available)} onClick={() => void next()}>{busy ? 'Inspecting…' : step === 1 ? 'Render Compose' : step === 3 ? 'Review deployment' : 'Continue'}</button> : <button type="button" className="hud-btn hud-btn-primary" disabled={busy || !preview?.deployable || reviewed !== JSON.stringify(draft)} onClick={() => void save()}>{busy ? 'Saving…' : appId ? 'Save reviewed revision' : 'Save app'}</button>}
    </div>
  </Airlock></div>;
}

function EnvironmentInput({ value, envs, onChange }: { value: string; envs: Environment[]; onChange: (id: string) => void }) {
  return <Field label="Runtime environment" hint="Provides configuration and secret values. Values are hidden in the preview."><Select value={value} onChange={(e) => onChange(e.target.value)} options={[{ value: '', label: 'No linked environment' }, ...envs.filter((e) => !e.id.startsWith('_')).map((e) => ({ value: e.id, label: e.name }))]} /></Field>;
}
