import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Card, Pill } from '../ui/atoms';
import { Icon } from '../ui/Icon';
import type { Environment } from '../../types/environment';

interface EnvironmentListProps {
  environments: Environment[];
  selected: Environment | null;
  onSelect: (env: Environment) => void;
}

/**
 * The promotion-path row of environment cards (e.g. dev → staging →
 * production). Clicking a card notifies the parent, which toggles the
 * secrets editor for that environment.
 */
export default function EnvironmentList({ environments, selected, onSelect }: EnvironmentListProps) {
  const t = useTheme();

  const colorFor = (name: string): string => {
    if (name.startsWith('prod')) return t.accent;
    if (name.startsWith('stag')) return t.warn;
    return t.cool;
  };

  return (
    <div style={{ display: 'flex', alignItems: 'stretch', flexWrap: 'wrap', gap: 14 }}>
      {environments
        .slice()
        .sort((a, b) => a.order - b.order)
        .map((env, i, arr) => {
          const color = colorFor(env.name);
          const isSelected = selected?.id === env.id;
          return (
            <span
              key={env.id}
              style={{ display: 'flex', alignItems: 'center', flex: '1 1 280px' }}
            >
              <Card
                onClick={() => onSelect(env)}
                style={{
                  flex: 1,
                  cursor: 'pointer',
                  borderTop: `3px solid ${color}`,
                  borderColor: hexA(color, 0.4),
                  background: isSelected ? hexA(color, 0.08) : hexA(color, 0.04),
                  boxShadow: isSelected ? `0 0 0 4px ${hexA(color, 0.12)}` : 'none',
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
