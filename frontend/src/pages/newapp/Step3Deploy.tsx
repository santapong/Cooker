import type { Dispatch, SetStateAction } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Pill, Toggle } from '../../components/ui/atoms';
import { Icon } from '../../components/ui/Icon';
import StepShell from './StepShell';
import type { EnvCard } from './types';

interface Step3DeployProps {
  envs: EnvCard[];
  setEnvs: Dispatch<SetStateAction<EnvCard[]>>;
}

export default function Step3Deploy({ envs, setEnvs }: Step3DeployProps) {
  const t = useTheme();
  return (
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
