import { useTheme } from '../../theme/ThemeProvider';
import { toneColor, type Tone } from './helpers';

export function StatusDot({
  tone,
  pulse = false,
  size = 8,
}: {
  tone: Tone;
  pulse?: boolean;
  size?: number;
}) {
  const t = useTheme();
  const c = toneColor(t, tone);
  return (
    <span
      style={{
        display: 'inline-block',
        width: size,
        height: size,
        borderRadius: 999,
        background: c,
        boxShadow: pulse ? `0 0 8px ${c}` : 'none',
        animation: pulse ? 'ccPulse 1.6s ease-out infinite' : undefined,
        flexShrink: 0,
      }}
    />
  );
}
