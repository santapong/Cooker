import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';

export function Select(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  const t = useTheme();
  const { style, className, children, ...rest } = props;
  return (
    <select
      {...rest}
      className={className ? `cc-field ${className}` : 'cc-field'}
      style={{
        width: '100%',
        padding: '9px 11px',
        background: hexA(t.surfaceSolid, 0.5),
        color: t.text,
        border: `1px solid ${t.line}`,
        borderRadius: 8,
        fontSize: 13.5,
        fontFamily: t.sans,
        outline: 'none',
        appearance: 'none',
        transition: 'border-color .15s, box-shadow .15s',
        ...style,
      }}
    >
      {children}
    </select>
  );
}
