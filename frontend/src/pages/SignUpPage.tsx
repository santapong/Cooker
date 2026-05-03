import { useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { authApi, type AuthMethods } from '../api/auth';
import { useAuth } from '../auth/OIDCProvider';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Input, Label } from '../components/ui/atoms';
import { Icon } from '../components/ui/Icon';
import {
  AuthShell,
  ErrorBanner,
  FootBadge,
  Heading,
  Sub,
} from './SignInPage';

const MIN_PASSWORD_LENGTH = 8;

export default function SignUpPage() {
  const t = useTheme();
  const navigate = useNavigate();
  const { signupLocal, isAuthenticated } = useAuth();

  const [methods, setMethods] = useState<AuthMethods | null>(null);
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
      navigate('/', { replace: true });
    }
  }, [isAuthenticated, navigate]);

  const passwordTooShort = password.length > 0 && password.length < MIN_PASSWORD_LENGTH;
  const passwordMismatch = confirm.length > 0 && confirm !== password;
  const canSubmit =
    !busy &&
    email.length > 0 &&
    password.length >= MIN_PASSWORD_LENGTH &&
    confirm === password;

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!canSubmit) return;
    setBusy(true);
    try {
      await signupLocal(email, password, name || undefined);
      navigate('/', { replace: true });
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  // If signup is disabled at the server, render a friendly explanation
  // rather than a broken form.
  if (methods && (!methods.local.enabled || !methods.local.allowSignup)) {
    return (
      <AuthShell>
        <Heading>Sign-up is closed.</Heading>
        <Sub>
          This Cooker instance doesn't allow self-signup. Ask an admin to create an account
          for you, then come back to <Link to="/signin" style={{ color: t.accent, fontWeight: 600 }}>sign in</Link>.
        </Sub>
        <Btn
          kind="secondary"
          onClick={() => navigate('/signin')}
          style={{ width: '100%', justifyContent: 'center', marginTop: 12 }}
        >
          ← Back to sign in
        </Btn>
        <FootBadge>OIDC · PKCE · SSO + email/password</FootBadge>
      </AuthShell>
    );
  }

  return (
    <AuthShell>
      <Heading>Set up your account.</Heading>
      <Sub>The first account on a fresh Cooker becomes admin. Subsequent sign-ups start as viewers.</Sub>

      <form onSubmit={onSubmit} style={{ display: 'flex', flexDirection: 'column' }}>
        <Label>Display name</Label>
        <Input
          type="text"
          autoComplete="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Jules Reyes"
        />
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
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          placeholder="at least 8 characters"
        />
        {passwordTooShort && (
          <Hint>Use at least {MIN_PASSWORD_LENGTH} characters.</Hint>
        )}
        <Label>Confirm password</Label>
        <Input
          type="password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          required
        />
        {passwordMismatch && <Hint tone="bad">Passwords don't match yet.</Hint>}

        {error && <ErrorBanner>{error}</ErrorBanner>}

        <Btn
          kind="primary"
          icon="spark"
          type="submit"
          disabled={!canSubmit}
          style={{ width: '100%', justifyContent: 'center', marginTop: 20 }}
        >
          {busy ? 'Cooking your account…' : 'Create account'}
        </Btn>

        <p style={{ fontSize: 13, color: t.textMute, marginTop: 18, textAlign: 'center' }}>
          Already have one?{' '}
          <Link
            to="/signin"
            style={{
              color: t.accent,
              fontWeight: 600,
              textDecoration: 'none',
              borderBottom: `1px solid ${hexA(t.accent, 0.4)}`,
            }}
          >
            Sign in
          </Link>
        </p>
      </form>

      <div
        style={{
          marginTop: 24,
          padding: 12,
          background: hexA(t.cool, 0.08),
          border: `1px solid ${hexA(t.cool, 0.3)}`,
          borderRadius: 8,
          fontSize: 12,
          color: t.textSoft,
          display: 'flex',
          gap: 10,
          alignItems: 'flex-start',
        }}
      >
        <Icon name="spark" size={14} style={{ color: t.cool, marginTop: 2, flexShrink: 0 }} />
        <div>
          Password auth is provided as a fallback when OIDC isn't set up. For team or production
          use, point Cooker at an OIDC IdP (Google, Keycloak, Okta) instead.
        </div>
      </div>

      <FootBadge>OIDC · PKCE · SSO + email/password</FootBadge>
    </AuthShell>
  );
}

function Hint({ children, tone = 'neutral' }: { children: React.ReactNode; tone?: 'neutral' | 'bad' }) {
  const t = useTheme();
  return (
    <div
      style={{
        fontSize: 11.5,
        fontFamily: t.mono,
        color: tone === 'bad' ? t.bad : t.textMute,
        marginTop: 4,
      }}
    >
      {children}
    </div>
  );
}
