import { useEffect, useState } from 'react';
import { useEnvironmentStore } from '../stores/environmentStore';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Card, EmptyState, PageHeader, Pill } from '../components/ui/atoms';
import { Icon } from '../components/ui/Icon';

export default function EnvironmentsPage() {
  const t = useTheme();
  const { environments, loading, fetchEnvironments, createEnvironment } = useEnvironmentStore();
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    fetchEnvironments();
  }, [fetchEnvironments]);

  const seed = async () => {
    setBusy(true);
    try {
      await createEnvironment({
        name: 'dev',
        order: 1,
        target: { type: 'namespace', clusterId: '', namespace: 'cooker-dev', kubeContext: '' },
        promotion: { strategy: 'auto' },
        variables: {},
      });
      await createEnvironment({
        name: 'staging',
        order: 2,
        target: { type: 'namespace', clusterId: '', namespace: 'cooker-staging', kubeContext: '' },
        promotion: { strategy: 'auto' },
        variables: {},
      });
      await createEnvironment({
        name: 'production',
        order: 3,
        target: { type: 'namespace', clusterId: '', namespace: 'cooker-prod', kubeContext: '' },
        promotion: { strategy: 'manual' },
        variables: {},
      });
    } finally {
      setBusy(false);
    }
  };

  const colorFor = (name: string): string => {
    if (name.startsWith('prod')) return t.accent;
    if (name.startsWith('stag')) return t.warn;
    return t.cool;
  };

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="promotion path"
        title="Environments"
        subtitle="The conveyor belt your services move along. Deploys land in the leftmost env first, then promote (auto or manual) to the next."
        actions={<Btn kind="primary" icon="plus">Add environment</Btn>}
      />

      {environments.length === 0 && !loading ? (
        <EmptyState
          title="No environments yet."
          body="Set up dev → staging → production so promotions have somewhere to flow."
          action={
            <Btn kind="primary" icon="spark" onClick={seed} disabled={busy}>
              {busy ? 'Setting up…' : 'Seed dev / staging / prod'}
            </Btn>
          }
        />
      ) : (
        <div
          style={{
            display: 'flex',
            alignItems: 'stretch',
            flexWrap: 'wrap',
            gap: 14,
          }}
        >
          {environments
            .slice()
            .sort((a, b) => a.order - b.order)
            .map((env, i, arr) => {
              const color = colorFor(env.name);
              return (
                <span
                  key={env.id}
                  style={{ display: 'flex', alignItems: 'center', flex: '1 1 280px' }}
                >
                  <Card
                    style={{
                      flex: 1,
                      borderTop: `3px solid ${color}`,
                      borderColor: hexA(color, 0.4),
                      background: hexA(color, 0.04),
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
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
                        {env.name}
                      </span>
                      <Pill tone={env.promotion.strategy === 'auto' ? 'good' : 'warn'}>
                        {env.promotion.strategy}
                      </Pill>
                    </div>
                    <div
                      style={{
                        fontFamily: t.serif,
                        fontSize: 22,
                        fontWeight: 500,
                        color: t.text,
                        letterSpacing: -0.3,
                        marginBottom: 12,
                      }}
                    >
                      {env.name === 'dev'
                        ? 'Development'
                        : env.name === 'staging'
                          ? 'Staging'
                          : env.name === 'production'
                            ? 'Production'
                            : env.name}
                    </div>
                    <Detail
                      label="Target"
                      value={
                        env.target.type === 'namespace'
                          ? `ns/${env.target.namespace}`
                          : `cluster/${env.target.clusterId}`
                      }
                    />
                    <Detail
                      label="Variables"
                      value={`${Object.keys(env.variables).length} keys`}
                    />
                  </Card>
                  {i < arr.length - 1 && (
                    <div
                      style={{
                        display: 'flex',
                        flexDirection: 'column',
                        alignItems: 'center',
                        color: t.textMute,
                        padding: '0 12px',
                        gap: 4,
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
                        promote
                      </span>
                    </div>
                  )}
                </span>
              );
            })}
        </div>
      )}
    </div>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  const t = useTheme();
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12.5, marginTop: 4 }}>
      <span style={{ color: t.textMute }}>{label}</span>
      <span style={{ fontFamily: t.mono, color: t.text }}>{value}</span>
    </div>
  );
}
