import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';

interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  pad?: number;
  /** left accent bar (e.g. stat tiles) */
  accent?: string;
}

export function Card({ children, style, pad = 20, accent, ...rest }: CardProps) {
  const t = useTheme();
  return (
    <div
      {...rest}
      style={{
        background: t.surface,
        backdropFilter: 'blur(10px)',
        WebkitBackdropFilter: 'blur(10px)',
        border: `1px solid ${t.line}`,
        borderLeft: accent ? `3px solid ${accent}` : `1px solid ${t.line}`,
        borderRadius: 14,
        padding: pad,
        boxShadow: `0 10px 30px ${hexA('#000', t.mode === 'dark' ? 0.32 : 0.08)}`,
        ...style,
      }}
    >
      {children}
    </div>
  );
}
