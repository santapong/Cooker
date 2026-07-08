import type { Dispatch, SetStateAction } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Pill } from '../../components/ui/atoms';
import StepShell from './StepShell';
import type { RecipeId } from './types';

interface Step2BuildProps {
  recipe: RecipeId;
  setRecipe: Dispatch<SetStateAction<RecipeId>>;
  detected: RecipeId | null;
}

export default function Step2Build({ recipe, setRecipe, detected }: Step2BuildProps) {
  const t = useTheme();
  return (
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
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  fontFamily: t.serif,
                  fontSize: 18,
                  fontWeight: 500,
                  color: t.text,
                }}
              >
                {r.title}
                {detected === r.id && <Pill tone="good">detected</Pill>}
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
  );
}
