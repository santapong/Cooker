import { Card } from '../../components/ui/atoms';
import { useTheme } from '../../theme/ThemeProvider';
import StepShell from './StepShell';
import type { EnvCard, RecipeId } from './types';

interface ReviewStepProps {
  name: string;
  repo: string;
  branch: string;
  recipe: RecipeId;
  envs: EnvCard[];
}

export default function ReviewStep({ name, repo, branch, recipe, envs }: ReviewStepProps) {
  return (
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
