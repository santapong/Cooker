import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Card, Input, Label, Pill, Toggle } from '../components/ui/atoms';
import { Icon } from '../components/ui/Icon';
import { useToastStore } from '../stores/toastStore';

interface Step {
  n: number;
  label: string;
  done: boolean;
  current?: boolean;
}

interface EnvCard {
  id: 'dev' | 'stg' | 'prod';
  title: string;
  sub: string;
  cluster: string;
  replicas: number;
  auto: boolean;
  selected: boolean;
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
  const [recipe, setRecipe] = useState<'go' | 'node-static' | 'worker'>('go');
  const [envs, setEnvs] = useState<EnvCard[]>([
    { id: 'dev', title: 'Development', sub: 'deploys on every push', cluster: 'k8s/dev', replicas: 1, auto: true, selected: true },
    { id: 'stg', title: 'Staging', sub: 'deploys after dev passes', cluster: 'k8s/staging', replicas: 2, auto: true, selected: true },
    { id: 'prod', title: 'Production', sub: 'needs your approval first', cluster: 'k8s/prod', replicas: 4, auto: false, selected: true },
  ]);
  const [busy, setBusy] = useState(false);

  const steps: Step[] = [
    { n: 1, label: 'Pick a source', done: stepIdx > 0 },
    { n: 2, label: 'Choose a recipe', done: stepIdx > 1 },
    { n: 3, label: 'Where to run', done: stepIdx > 2, current: stepIdx === 2 },
    { n: 4, label: 'Review & cook', done: stepIdx > 3 },
  ].map((s, i) => ({ ...s, current: i === stepIdx }));

  const next = () => setStepIdx((s) => Math.min(TOTAL_STEPS - 1, s + 1));
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
        <StepShell
          eyebrow="Step 1"
          title="What are you deploying?"
          body="Point Cooker at a GitHub repo. We'll detect the language and suggest a recipe in the next step."
        >
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 }}>
            <Card>
              <Label>App name</Label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="checkout-api"
              />
            </Card>
            <Card>
              <Label>GitHub repo (owner/name)</Label>
              <Input
                value={repo}
                onChange={(e) => setRepo(e.target.value)}
                placeholder="octocat/Hello-World"
              />
              <Label>Branch</Label>
              <Input
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder="main"
              />
            </Card>
          </div>
        </StepShell>
      )}

      {stepIdx === 1 && (
        <StepShell
          eyebrow="Step 2"
          title="Pick a recipe."
          body="Pre-baked stages for the most common service shapes. You can edit them later in Pro mode."
        >
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 14 }}>
            {(
              [
                { id: 'go', title: 'Go service', sub: 'compiled binary · k8s', stages: 'build → test → push → deploy' },
                { id: 'node-static', title: 'Static site', sub: 'Node build · CDN', stages: 'build → push → deploy' },
                { id: 'worker', title: 'Background worker', sub: 'queue consumer · k8s', stages: 'build → test → push → deploy' },
              ] as const
            ).map((r) => {
              const selected = recipe === r.id;
              return (
                <div
                  key={r.id}
                  onClick={() => setRecipe(r.id)}
                  style={{
                    background: t.surface,
                    border: `1.5px solid ${selected ? t.accent : t.line}`,
                    borderRadius: 12,
                    padding: 18,
                    cursor: 'pointer',
                    boxShadow: selected
                      ? `0 0 0 4px ${hexA(t.accent, 0.12)}`
                      : 'none',
                  }}
                >
                  <div
                    style={{
                      fontFamily: t.serif,
                      fontSize: 18,
                      fontWeight: 500,
                      color: t.text,
                    }}
                  >
                    {r.title}
                  </div>
                  <div style={{ fontSize: 13, color: t.textSoft, marginTop: 4 }}>{r.sub}</div>
                  <div
                    style={{
                      fontFamily: t.mono,
                      fontSize: 11,
                      color: t.textMute,
                      marginTop: 14,
                      letterSpacing: 0.4,
                    }}
                  >
                    {r.stages}
                  </div>
                </div>
              );
            })}
          </div>
        </StepShell>
      )}

      {stepIdx === 2 && (
        <StepShell
          eyebrow="Step 3"
          title="Where should this service run?"
          body={
            <>
              We'll set up a path from <strong style={{ color: t.text }}>development</strong>{' '}
              through <strong style={{ color: t.text }}>staging</strong> and into{' '}
              <strong style={{ color: t.text }}>production</strong>. You can always change this
              later.
            </>
          }
        >
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(3, 1fr)',
              gap: 16,
              marginBottom: 28,
            }}
          >
            {envs.map((env) => {
              const color = env.id === 'prod' ? t.accent : env.id === 'stg' ? t.warn : t.cool;
              return (
                <div
                  key={env.id}
                  onClick={() =>
                    setEnvs((es) =>
                      es.map((e) => (e.id === env.id ? { ...e, selected: !e.selected } : e)),
                    )
                  }
                  style={{
                    background: t.surface,
                    border: `1.5px solid ${env.selected ? color : t.line}`,
                    borderRadius: 12,
                    padding: 18,
                    position: 'relative',
                    overflow: 'hidden',
                    boxShadow: env.selected ? `0 0 0 4px ${hexA(color, 0.12)}` : 'none',
                    cursor: 'pointer',
                  }}
                >
                  <div
                    style={{
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      right: 0,
                      height: 3,
                      background: color,
                    }}
                  />
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                    <span
                      style={{
                        fontFamily: t.mono,
                        fontSize: 10.5,
                        letterSpacing: 1.4,
                        textTransform: 'uppercase',
                        color,
                        fontWeight: 700,
                      }}
                    >
                      {env.id}
                    </span>
                    {env.selected && <Pill tone="good">on</Pill>}
                  </div>
                  <div
                    style={{
                      fontFamily: t.serif,
                      fontSize: 22,
                      fontWeight: 500,
                      color: t.text,
                      letterSpacing: -0.3,
                    }}
                  >
                    {env.title}
                  </div>
                  <div
                    style={{
                      fontSize: 13,
                      color: t.textSoft,
                      marginTop: 4,
                      marginBottom: 14,
                    }}
                  >
                    {env.sub}
                  </div>

                  <div
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 8,
                      fontSize: 12.5,
                    }}
                  >
                    <Row label="Cluster">
                      <span style={{ fontFamily: t.mono, color: t.text }}>{env.cluster}</span>
                    </Row>
                    <Row label="Replicas">
                      <span style={{ fontFamily: t.mono, color: t.text }}>{env.replicas}</span>
                    </Row>
                    <Row label="Promotion">
                      <Toggle
                        on={env.auto}
                        label={env.auto ? 'auto' : 'manual'}
                        onClick={() =>
                          setEnvs((es) =>
                            es.map((e) =>
                              e.id === env.id ? { ...e, auto: !e.auto } : e,
                            ),
                          )
                        }
                      />
                    </Row>
                  </div>
                </div>
              );
            })}
          </div>

          <div
            style={{
              background: t.surface,
              border: `1px solid ${t.line}`,
              borderRadius: 12,
              padding: 20,
              marginBottom: 28,
            }}
          >
            <div
              style={{
                fontFamily: t.mono,
                fontSize: 10.5,
                letterSpacing: 1.2,
                textTransform: 'uppercase',
                color: t.textMute,
                marginBottom: 14,
              }}
            >
              Your path to production
            </div>
            <PathDiagram envs={envs} />
          </div>

          <TipCard>
            Leave production on <strong>manual approval</strong> for now. Cooker will let you
            click a button when you're ready — no surprises.
          </TipCard>
        </StepShell>
      )}

      {stepIdx === 3 && (
        <StepShell
          eyebrow="Step 4"
          title="Ready to cook?"
          body="Here's the summary. Hit the button and Cooker will provision pipelines and webhooks for you."
        >
          <Card pad={20}>
            <Summary label="App name" value={name || '—'} />
            <Summary label="Repo" value={repo ? `github.com/${repo}@${branch}` : '—'} />
            <Summary label="Recipe" value={recipe} />
            <Summary
              label="Environments"
              value={envs
                .filter((e) => e.selected)
                .map((e) => `${e.id}(${e.auto ? 'auto' : 'manual'})`)
                .join(' · ')}
            />
          </Card>
        </StepShell>
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

function StepShell({
  eyebrow,
  title,
  body,
  children,
}: {
  eyebrow: string;
  title: string;
  body: React.ReactNode;
  children: React.ReactNode;
}) {
  const t = useTheme();
  return (
    <div>
      <div style={{ marginBottom: 28, maxWidth: 720 }}>
        <div
          style={{
            fontFamily: t.mono,
            fontSize: 11,
            letterSpacing: 1.6,
            textTransform: 'uppercase',
            color: t.accent,
            marginBottom: 10,
          }}
        >
          {eyebrow}
        </div>
        <h1
          style={{
            fontFamily: t.serif,
            fontSize: 36,
            fontWeight: 500,
            color: t.text,
            letterSpacing: -0.6,
            lineHeight: 1.05,
            margin: 0,
          }}
        >
          {title}
        </h1>
        <p style={{ fontSize: 15, color: t.textSoft, marginTop: 12, lineHeight: 1.55 }}>
          {body}
        </p>
      </div>
      {children}
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  const t = useTheme();
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
      <span style={{ color: t.textMute }}>{label}</span>
      {children}
    </div>
  );
}

function Summary({ label, value }: { label: string; value: string }) {
  const t = useTheme();
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        padding: '10px 0',
        borderBottom: `1px solid ${t.lineSoft}`,
      }}
    >
      <span
        style={{
          fontFamily: t.mono,
          fontSize: 11,
          letterSpacing: 1,
          textTransform: 'uppercase',
          color: t.textMute,
        }}
      >
        {label}
      </span>
      <span style={{ fontFamily: t.mono, fontSize: 13, color: t.text }}>{value}</span>
    </div>
  );
}

function PathDiagram({ envs }: { envs: EnvCard[] }) {
  const t = useTheme();
  const items = envs
    .filter((e) => e.selected)
    .map((e) => ({
      l: e.title.replace('Development', 'Dev').replace('Staging', 'Staging').replace('Production', 'Prod'),
      c: e.id === 'prod' ? t.accent : e.id === 'stg' ? t.warn : t.cool,
      sub: e.auto ? 'auto' : 'approval',
    }));
  if (items.length === 0) return <div style={{ color: t.textMute, fontSize: 13 }}>Select at least one environment.</div>;
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
      {items.map((s, i) => (
        <span key={s.l} style={{ display: 'contents' }}>
          <div
            style={{
              flex: 1,
              padding: '16px 18px',
              background: hexA(s.c, 0.08),
              border: `1.5px solid ${hexA(s.c, 0.4)}`,
              borderRadius: 10,
            }}
          >
            <div style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
              {s.l}
            </div>
            <div
              style={{
                fontFamily: t.mono,
                fontSize: 10.5,
                color: s.c,
                letterSpacing: 0.6,
                textTransform: 'uppercase',
                marginTop: 2,
              }}
            >
              {s.sub}
            </div>
          </div>
          {i < items.length - 1 && (
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                color: t.textMute,
                gap: 2,
              }}
            >
              <Icon name="arrow" size={20} />
              <span
                style={{
                  fontFamily: t.mono,
                  fontSize: 9.5,
                  letterSpacing: 0.8,
                  textTransform: 'uppercase',
                }}
              >
                {items[i + 1].sub === 'auto' ? 'if green' : 'you approve'}
              </span>
            </div>
          )}
        </span>
      ))}
    </div>
  );
}

function TipCard({ children }: { children: React.ReactNode }) {
  const t = useTheme();
  return (
    <div
      style={{
        display: 'flex',
        gap: 14,
        padding: 16,
        background: hexA(t.accent, 0.06),
        border: `1px solid ${hexA(t.accent, 0.3)}`,
        borderRadius: 10,
      }}
    >
      <div
        style={{
          width: 28,
          height: 28,
          borderRadius: 999,
          flexShrink: 0,
          background: t.accent,
          color: '#fff',
          display: 'grid',
          placeItems: 'center',
        }}
      >
        <Icon name="spark" size={14} />
      </div>
      <div>
        <div style={{ fontSize: 13, fontWeight: 600, color: t.text }}>New to deploys?</div>
        <div
          style={{
            fontSize: 13,
            color: t.textSoft,
            marginTop: 4,
            lineHeight: 1.55,
          }}
        >
          {children}
        </div>
      </div>
    </div>
  );
}
