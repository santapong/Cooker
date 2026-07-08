import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  const t = useTheme();
  const { style, className, ...rest } = props;
  return (
    <input
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
        transition: 'border-color .15s, box-shadow .15s',
        ...style,
      }}
    />
  );
}
