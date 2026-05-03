import { useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate, useLocation } from 'react-router-dom';
import { authApi, type AuthMethods } from '../api/auth';
import { useAuth } from '../auth/OIDCProvider';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Input, Label } from '../components/ui/atoms';
import { CookerMark, Icon } from '../components/ui/Icon';

export default function SignInPage() {
  const t = useTheme();
  const navigate = useNavigate();
  const location = useLocation();
  const { login, signinLocal, isAuthenticated } = useAuth();

  const [methods, setMethods] = useState<AuthMethods | null>(null);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // After a successful login, send users to where they were headed
  // (or / if they came here directly).
  const returnTo =
    (location.state as { from?: string } | null)?.from ?? '/';

  useEffect(() => {
    authApi
      .methods()
      .then(setMethods)
      .catch(() =>
        setMethods({ oidc: { enabled: true }, local: { enabled: false, allowSignup: false } }),
      );
  }, []);

  useEffect(() => {
    if (isAuthenticated) {
      navigate(returnTo, { replace: true });
    }
  }, [isAuthenticated, navigate, returnTo]);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await signinLocal(email, password);
      navigate(returnTo, { replace: true });
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell>
      <Heading>Welcome back.</Heading>
      <Sub>Sign in to keep cooking.</Sub>

      {methods?.oidc.enabled && (
        <>
          <Btn
            kind="primary"
            icon="logout"
            onClick={login}
            style={{ width: '100%', justifyContent: 'center', marginTop: 8 }}
          >
            Sign in with SSO
          </Btn>
          {methods.local.enabled && <OrDivider />}
        </>
      )}

      {methods?.local.enabled && (
        <form onSubmit={onSubmit} style={{ display: 'flex', flexDirection: 'column' }}>
          <Label>Email</Label>
          <Input
            type="email"
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            placeholder="you@example.com"
          />
          <Label>Password</Label>
          <Input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            placeholder="••••••••"
          />
          {error && <ErrorBanner>{error}</ErrorBanner>}
          <Btn
            kind="ink"
            icon="arrow"
            type="submit"
            disabled={busy || !email || !password}
            style={{ width: '100%', justifyContent: 'center', marginTop: 18 }}
          >
            {busy ? 'Signing in…' : 'Sign in'}
          </Btn>

          {methods.local.allowSignup && (
            <p style={{ fontSize: 13, color: t.textMute, marginTop: 18, textAlign: 'center' }}>
              No account?{' '}
              <Link
                to="/signup"
                style={{
                  color: t.accent,
                  fontWeight: 600,
                  textDecoration: 'none',
                  borderBottom: `1px solid ${hexA(t.accent, 0.4)}`,
                }}
              >
                Create one
              </Link>
            </p>
          )}
        </form>
      )}

      {methods && !methods.oidc.enabled && !methods.local.enabled && (
        <div
          style={{
            marginTop: 20,
            padding: 14,
            background: hexA(t.warn, 0.1),
            border: `1px solid ${hexA(t.warn, 0.4)}`,
            borderRadius: 8,
            color: t.warn,
            fontSize: 13,
            display: 'flex',
            gap: 10,
            alignItems: 'flex-start',
          }}
        >
          <Icon name="cog" size={14} style={{ marginTop: 2 }} />
          <div>
            No auth methods enabled on this server. Set <code>COOKER_OIDC_ENABLED=true</code> or{' '}
            <code>COOKER_LOCAL_AUTH_ENABLED=true</code>.
          </div>
        </div>
      )}

      <FootBadge>OIDC · PKCE · SSO + email/password</FootBadge>
    </AuthShell>
  );
}

// --- Shared layout primitives. Kept in this file because they're
// only used by the two auth pages and inlining is cheaper than another
// shared module for two components.

export function AuthShell({ children }: { children: React.ReactNode }) {
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
        fontFamily: t.sans,
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
          maxWidth: 460,
          background: t.surface,
          border: `1px solid ${t.line}`,
          borderRadius: 16,
          padding: 40,
          boxShadow: `0 12px 32px ${hexA('#000', 0.4)}`,
        }}
      >
        <Brand />
        {children}
      </div>
    </div>
  );
}

function Brand() {
  const t = useTheme();
  return (
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
  );
}

export function Heading({ children }: { children: React.ReactNode }) {
  const t = useTheme();
  return (
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
      {children}
    </h1>
  );
}

export function Sub({ children }: { children: React.ReactNode }) {
  const t = useTheme();
  return (
    <p style={{ fontSize: 14, color: t.textSoft, marginTop: 0, marginBottom: 24, lineHeight: 1.55 }}>
      {children}
    </p>
  );
}

export function ErrorBanner({ children }: { children: React.ReactNode }) {
  const t = useTheme();
  return (
    <div
      style={{
        marginTop: 14,
        padding: '10px 12px',
        background: hexA(t.bad, 0.08),
        border: `1px solid ${hexA(t.bad, 0.4)}`,
        borderRadius: 8,
        color: t.bad,
        fontSize: 12.5,
        fontFamily: t.mono,
      }}
    >
      {children}
    </div>
  );
}

export function FootBadge({ children }: { children: React.ReactNode }) {
  const t = useTheme();
  return (
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
      {children}
    </div>
  );
}

function OrDivider() {
  const t = useTheme();
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        margin: '20px 0 14px',
        color: t.textMute,
        fontSize: 11,
        fontFamily: t.mono,
        letterSpacing: 1.2,
        textTransform: 'uppercase',
      }}
    >
      <span style={{ flex: 1, height: 1, background: t.line }} />
      <span>or</span>
      <span style={{ flex: 1, height: 1, background: t.line }} />
    </div>
  );
}

