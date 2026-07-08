import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';

export function Toggle({ on, label, onClick }: { on: boolean; label?: string; onClick?: () => void }) {
  const t = useTheme();
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        cursor: onClick ? 'pointer' : 'default',
      }}
      onClick={onClick}
    >
      <span
        style={{
          width: 30,
          height: 16,
          borderRadius: 999,
          position: 'relative',
          background: on ? t.good : t.line,
          transition: 'all .2s',
        }}
      >
        <span
          style={{
            position: 'absolute',
            top: 2,
            left: on ? 16 : 2,
            width: 12,
            height: 12,
            borderRadius: 999,
            background: '#fff',
            transition: 'all .2s',
            boxShadow: `0 1px 2px ${hexA('#000', 0.2)}`,
          }}
        />
      </span>
      {label && (
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 10.5,
            color: on ? t.good : t.textMute,
            textTransform: 'uppercase',
            letterSpacing: 0.6,
          }}
        >
          {label}
        </span>
      )}
    </span>
  );
}
