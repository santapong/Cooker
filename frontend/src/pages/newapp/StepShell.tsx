import { useTheme } from '../../theme/ThemeProvider';

export default function StepShell({
  eyebrow,
  title,
  body,
  children,
}: {
  eyebrow: string;
  title: string;
  body: React.ReactNode;
  children: React.ReactNode;
}) {
  const t = useTheme();
  return (
    <div>
      <div style={{ marginBottom: 28, maxWidth: 720 }}>
        <div
          style={{
            fontFamily: t.mono,
            fontSize: 11,
            letterSpacing: 1.6,
            textTransform: 'uppercase',
            color: t.accent,
            marginBottom: 10,
          }}
        >
          {eyebrow}
        </div>
        <h1
          style={{
            fontFamily: t.serif,
            fontSize: 36,
            fontWeight: 500,
            color: t.text,
            letterSpacing: -0.6,
            lineHeight: 1.05,
            margin: 0,
          }}
        >
          {title}
        </h1>
        <p style={{ fontSize: 15, color: t.textSoft, marginTop: 12, lineHeight: 1.55 }}>
          {body}
        </p>
      </div>
      {children}
    </div>
  );
}
