import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';

export function HealthBar({ value }: { value: number }) {
  const t = useTheme();
  const c = value > 95 ? t.good : value > 80 ? t.warn : t.bad;
  return (
    <div
      style={{
        width: 100,
        height: 5,
        background: t.line,
        borderRadius: 3,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          width: `${Math.max(0, Math.min(100, value))}%`,
          height: '100%',
          background: c,
          borderRadius: 3,
          boxShadow: `0 0 8px ${hexA(c, 0.6)}`,
        }}
      />
    </div>
  );
}
