import { useEffect, useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { authApi, type AuthMethods } from '../api/auth';
import { useAuth } from '../auth/OIDCProvider';
import Airlock from '../components/airlock/Airlock';
import { Actions, Field, FormError, TextInput } from '../components/ui/form';

const FALLBACK: AuthMethods = { oidc: { enabled: true }, local: { enabled: false, allowSignup: false } };

export default function SignInPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { login, signinLocal, isAuthenticated } = useAuth();
  const [methods, setMethods] = useState<AuthMethods | null>(null);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const returnTo = (location.state as { from?: string } | null)?.from ?? '/';

  useEffect(() => {
    authApi.methods().then(setMethods).catch(() => setMethods(FALLBACK));
  }, []);
  useEffect(() => {
    if (isAuthenticated) navigate(returnTo, { replace: true });
  }, [isAuthenticated, navigate, returnTo]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await signinLocal(email, password);
      navigate(returnTo, { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  };

  const oidc = methods?.oidc.enabled ?? false;
  const local = methods?.local.enabled ?? false;

  return (
    <Airlock full title="Sign in">
      {!methods && <p>Checking sign-in methods…</p>}
      {oidc && (
        <button type="button" className="hud-btn hud-btn-primary btn-block" onClick={login}>
          Continue with single sign-on
        </button>
      )}
      {oidc && local && <span className="airlock-or">or</span>}
      {local && (
        <form onSubmit={submit}>
          <Field label="Email">
            <TextInput type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus={!oidc} />
          </Field>
          <Field label="Password">
            <TextInput type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </Field>
          <FormError>{error}</FormError>
          <Actions>
            <button type="submit" className="hud-btn hud-btn-primary btn-block" disabled={busy}>
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </Actions>
        </form>
      )}
      {methods && !oidc && !local && <p>No sign-in method is enabled on this server.</p>}
      <div className="airlock-foot">
        {methods?.local.allowSignup && <Link to="/signup">Create an account</Link>}
        <span>Cooker · CI/CD you can see.</span>
      </div>
    </Airlock>
  );
}
