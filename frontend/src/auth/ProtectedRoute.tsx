import { type ReactNode } from 'react';
import { useAuth } from './OIDCProvider';
import { SkeletonStack } from '../components/Skeleton';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn } from '../components/ui/atoms';
import { CookerMark } from '../components/ui/Icon';

interface Props {
  children: ReactNode;
  requiredRoles?: string[];
}

export default function ProtectedRoute({ children, requiredRoles }: Props) {
  const { user, isAuthenticated, isLoading, login } = useAuth();
  const t = useTheme();

  if (isLoading) {
    return (
      <div
        style={{
          padding: 40,
          maxWidth: 640,
          margin: '0 auto',
          display: 'flex',
          flexDirection: 'column',
          gap: 16,
          background: t.bg,
          minHeight: '100vh',
        }}
        aria-busy="true"
        aria-live="polite"
      >
        <SkeletonStack rows={1} height={28} />
        <SkeletonStack rows={4} height={14} />
      </div>
    );
  }

  if (!isAuthenticated) {
    return <SignIn onSignIn={login} />;
  }

  if (requiredRoles && user) {
    const hasRole = requiredRoles.some((role) => user.roles.includes(role));
    if (!hasRole) {
      return (
        <div
          style={{
            minHeight: '100vh',
            background: t.bg,
            color: t.text,
            display: 'grid',
            placeItems: 'center',
            padding: 40,
            textAlign: 'center',
          }}
        >
          <div>
            <h2 style={{ fontFamily: t.serif, fontSize: 28, fontWeight: 500, color: t.text, margin: 0 }}>
              Access denied
            </h2>
            <p style={{ color: t.textSoft, marginTop: 12 }}>
              You need one of these roles: {requiredRoles.join(', ')}
            </p>
          </div>
        </div>
      );
    }
  }

  return <>{children}</>;
}

function SignIn({ onSignIn }: { onSignIn: () => void }) {
  const t = useTheme();
  return (
    <div
      style={{
        minHeight: '100vh',
        background: t.bg,
        color: t.text,
        display: 'grid',
        placeItems: 'center',
        padding: 40,
        position: 'relative',
        overflow: 'hidden',
      }}
    >
      <div
        aria-hidden
        style={{
          position: 'absolute',
          inset: 0,
          background: `radial-gradient(circle at 30% 20%, ${hexA(t.accent, 0.15)} 0%, transparent 60%)`,
          pointerEvents: 'none',
        }}
      />
      <div
        style={{
          position: 'relative',
          width: '100%',
          maxWidth: 440,
          background: t.surface,
          border: `1px solid ${t.line}`,
          borderRadius: 16,
          padding: 40,
          boxShadow: `0 12px 32px ${hexA('#000', 0.4)}`,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 28 }}>
          <CookerMark color={t.accent} size={36} />
          <div>
            <div
              style={{
                fontFamily: t.serif,
                fontSize: 28,
                fontWeight: 600,
                color: t.text,
                lineHeight: 1,
                letterSpacing: -0.4,
              }}
            >
              Cooker
            </div>
            <div
              style={{
                fontFamily: t.mono,
                fontSize: 10,
                letterSpacing: 1.6,
                color: t.textMute,
                textTransform: 'uppercase',
                marginTop: 4,
              }}
            >
              build · ship · run
            </div>
          </div>
        </div>

        <h1
          style={{
            fontFamily: t.serif,
            fontSize: 32,
            fontWeight: 500,
            color: t.text,
            margin: '0 0 8px',
            letterSpacing: -0.6,
            lineHeight: 1.05,
          }}
        >
          Welcome back.
        </h1>
        <p style={{ fontSize: 14, color: t.textSoft, marginTop: 0, marginBottom: 28, lineHeight: 1.55 }}>
          Sign in with your single sign-on provider to keep cooking.
        </p>

        <Btn kind="primary" icon="logout" onClick={onSignIn} style={{ width: '100%', justifyContent: 'center' }}>
          Sign in with SSO
        </Btn>

        <div
          style={{
            marginTop: 24,
            paddingTop: 20,
            borderTop: `1px solid ${t.lineSoft}`,
            fontFamily: t.mono,
            fontSize: 11,
            color: t.textMute,
            letterSpacing: 0.6,
            textAlign: 'center',
          }}
        >
          OIDC · PKCE · single sign-on
        </div>
      </div>
    </div>
  );
}
