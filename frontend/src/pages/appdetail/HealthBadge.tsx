import type { AppModel } from '../../types/app';
import { Pill } from '../../components/ui/atoms';

// HealthBadge renders the post-deploy readiness verdict written by
// the backend AppHealthChecker. "unknown" is the default until the
// first probe runs (or when the target kind has no probe wired) —
// shown as a muted neutral pill so operators learn the page state
// without being alarmed.
export default function HealthBadge({ app }: { app: AppModel }) {
  const status = app.healthStatus ?? 'unknown';
  const tone: 'good' | 'bad' | 'warn' | 'neutral' =
    status === 'healthy'
      ? 'good'
      : status === 'failed'
        ? 'bad'
        : status === 'degraded'
          ? 'warn'
          : 'neutral';
  const label =
    status === 'healthy'
      ? 'healthy'
      : status === 'failed'
        ? 'unhealthy'
        : status === 'degraded'
          ? 'degraded'
          : 'health unknown';
  return (
    <span style={{ marginLeft: 10, display: 'inline-flex', verticalAlign: 'middle' }} title={app.healthMessage ?? ''}>
      <Pill tone={tone}>{label}</Pill>
    </span>
  );
}
