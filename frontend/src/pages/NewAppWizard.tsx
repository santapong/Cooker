import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import { useTheme } from '../theme/ThemeProvider';
import { Btn } from '../components/ui/atoms';
import { Icon } from '../components/ui/Icon';
import { useToastStore } from '../stores/toastStore';
import Step1Source from './newapp/Step1Source';
import Step2Build from './newapp/Step2Build';
import Step3Deploy from './newapp/Step3Deploy';
import ReviewStep from './newapp/ReviewStep';
import type { EnvCard, RecipeId } from './newapp/types';

interface Step {
  n: number;
  label: string;
  done: boolean;
  current?: boolean;
}

const TOTAL_STEPS = 4;

export default function NewAppWizard() {
  const t = useTheme();
  const navigate = useNavigate();
  const pushToast = useToastStore((s) => s.push);

  const [stepIdx, setStepIdx] = useState(0);
  const [name, setName] = useState('');
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('main');
  const [recipe, setRecipe] = useState<RecipeId>('go');
  const [envs, setEnvs] = useState<EnvCard[]>([
    { id: 'dev', title: 'Development', sub: 'deploys on every push', cluster: 'k8s/dev', replicas: 1, auto: true, selected: true },
    { id: 'stg', title: 'Staging', sub: 'deploys after dev passes', cluster: 'k8s/staging', replicas: 2, auto: true, selected: true },
    { id: 'prod', title: 'Production', sub: 'needs your approval first', cluster: 'k8s/prod', replicas: 4, auto: false, selected: true },
  ]);
  const [busy, setBusy] = useState(false);
  const [detected, setDetected] = useState<RecipeId | null>(null);

  const steps: Step[] = [
    { n: 1, label: 'Pick a source', done: stepIdx > 0 },
    { n: 2, label: 'Choose a recipe', done: stepIdx > 1 },
    { n: 3, label: 'Where to run', done: stepIdx > 2, current: stepIdx === 2 },
    { n: 4, label: 'Review & cook', done: stepIdx > 3 },
  ].map((s, i) => ({ ...s, current: i === stepIdx }));

  const next = () => {
    // Leaving the source step: inspect the repo in the background and
    // pre-select the matching recipe. Non-blocking — the user can keep
    // going; failure just means they pick manually.
    if (stepIdx === 0 && repo.trim() && !detected) {
      appsApi
        .detectBuild(repo.trim(), branch.trim() || 'main')
        .then((res) => {
          setRecipe(res.suggestedRecipe);
          setDetected(res.suggestedRecipe);
        })
        .catch(() => {
          pushToast({
            kind: 'info',
            message: 'Could not inspect the repo — pick a recipe manually.',
          });
        });
    }
    setStepIdx((s) => Math.min(TOTAL_STEPS - 1, s + 1));
  };
  const back = () => setStepIdx((s) => Math.max(0, s - 1));

  const submit = async () => {
    setBusy(true);
    try {
      const env = envs.find((e) => e.selected);
      await appsApi.create({
        name,
        githubRepo: repo,
        branch,
        deployTarget: {
          kind: 'kubernetes',
          namespace: env?.cluster.split('/')[1] ?? 'default',
        },
        autoDeploy: envs.find((e) => e.id === 'dev')?.auto ?? true,
      });
      pushToast({ kind: 'success', message: `App "${name}" created.` });
      navigate('/apps');
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      style={{
        height: '100%',
        overflow: 'auto',
        padding: '32px 40px 60px',
        maxWidth: 1100,
        margin: '0 auto',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 0, marginBottom: 36 }}>
        {steps.map((s, i) => (
          <span key={s.n} style={{ display: 'flex', alignItems: 'center', flex: i < steps.length - 1 ? 1 : 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <div
                style={{
                  width: 28,
                  height: 28,
                  borderRadius: 999,
                  background: s.done ? t.good : s.current ? t.accent : t.surfaceAlt,
                  color: s.done || s.current ? '#fff' : t.textMute,
                  display: 'grid',
                  placeItems: 'center',
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 12,
                  border: `1px solid ${s.done ? t.good : s.current ? t.accent : t.line}`,
                }}
              >
                {s.done ? <Icon name="check" size={14} /> : s.n}
              </div>
              <span
                style={{
                  fontSize: 13,
                  fontWeight: s.current ? 600 : 500,
                  color: s.current ? t.text : s.done ? t.textSoft : t.textMute,
                  whiteSpace: 'nowrap',
                }}
              >
                {s.label}
              </span>
            </div>
            {i < steps.length - 1 && (
              <div
                style={{
                  flex: 1,
                  height: 1,
                  background: s.done ? t.good : t.line,
                  margin: '0 14px',
                }}
              />
            )}
          </span>
        ))}
      </div>

      {stepIdx === 0 && (
        <Step1Source
          name={name}
          setName={setName}
          repo={repo}
          setRepo={setRepo}
          branch={branch}
          setBranch={setBranch}
        />
      )}

      {stepIdx === 1 && (
        <Step2Build recipe={recipe} setRecipe={setRecipe} detected={detected} />
      )}

      {stepIdx === 2 && <Step3Deploy envs={envs} setEnvs={setEnvs} />}

      {stepIdx === 3 && (
        <ReviewStep name={name} repo={repo} branch={branch} recipe={recipe} envs={envs} />
      )}

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          paddingTop: 20,
          marginTop: 28,
          borderTop: `1px solid ${t.line}`,
        }}
      >
        <Btn kind="ghost" onClick={back} disabled={stepIdx === 0}>
          ← Back
        </Btn>
        <div style={{ flex: 1 }} />
        <span style={{ fontFamily: t.mono, fontSize: 11, color: t.textMute }}>
          Step {stepIdx + 1} of {TOTAL_STEPS}
        </span>
        {stepIdx < TOTAL_STEPS - 1 ? (
          <Btn
            kind="primary"
            icon="arrow"
            onClick={next}
            disabled={stepIdx === 0 && (!name || !repo)}
          >
            Continue
          </Btn>
        ) : (
          <Btn kind="primary" icon="play" onClick={submit} disabled={busy}>
            {busy ? 'Cooking…' : 'Cook it'}
          </Btn>
        )}
      </div>
    </div>
  );
}
