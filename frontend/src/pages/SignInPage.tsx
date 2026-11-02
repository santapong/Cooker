import { useEffect, useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../auth/OIDCProvider';
import { isOIDCSignInConfigured, safeAuthReturnPath, type SocialProvider } from '../auth/socialSignIn';
import SocialSignIn from '../components/airlock/SocialSignIn';
import Airlock from '../components/airlock/Airlock';
import AuthHelp from '../components/airlock/AuthHelp';
import PasswordField from '../components/airlock/PasswordField';
import useAuthMethods from '../components/airlock/useAuthMethods';
import { Actions, Field, FormError, TextInput } from '../components/ui/form';

export default function SignInPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { login, signinSocial, signinLocal, isAuthenticated } = useAuth();
  const { methods, discoveryError, retry } = useAuthMethods();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const returnTo = safeAuthReturnPath((location.state as { from?: string } | null)?.from);

  useEffect(() => {
    if (isAuthenticated) navigate(returnTo, { replace: true });
  }, [isAuthenticated, navigate, returnTo]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
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

  const oidcReady = isOIDCSignInConfigured();
  const oidc = (methods?.oidc.enabled ?? false) && oidcReady;
  const local = methods?.local.enabled ?? false;

  const social = async (provider: SocialProvider) => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await signinSocial(provider, returnTo);
    } catch {
      setError('We could not start that sign-in. Try again or use another available method.');
      setBusy(false);
    }
  };

  return (
    <Airlock full title="Sign in" description="Welcome back. See what’s ready to ship.">
      {!methods && !discoveryError && <p className="auth-status" role="status">Checking sign-in methods…</p>}
      {discoveryError && (
        <div className="auth-status" role="alert">
          <p>We could not check this server’s sign-in methods. Check your connection and try again.</p>
          <button type="button" className="hud-btn btn-block" onClick={retry}>Try again</button>
        </div>
      )}
      {methods && <SocialSignIn methods={methods} oidcReady={oidcReady} busy={busy} onSignIn={(provider) => { void social(provider); }} />}
      <FormError>{error}</FormError>
      {oidc && (
        <button type="button" className={`hud-btn ${local ? 'hud-btn-secondary' : 'hud-btn-primary'} btn-block`} onClick={login} disabled={busy}>
          Continue with single sign-on
        </button>
      )}
      {oidc && local && <span className="airlock-or">or use your local account</span>}
      {local && (
        <form onSubmit={submit} aria-busy={busy}>
          <Field label="Email">
            <TextInput type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus={!oidc} disabled={busy} />
          </Field>
          <PasswordField label="Password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required disabled={busy} />
          <Actions>
            <button type="submit" className="hud-btn hud-btn-primary btn-block" disabled={busy}>
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </Actions>
        </form>
      )}
      {methods && !oidc && !local && <p className="auth-status" role="status">No sign-in method is enabled on this server. Contact your Cooker administrator for access.</p>}
      {local && methods?.local.allowSignup && <div className="airlock-foot"><Link to="/signup">Create an account</Link></div>}
      <AuthHelp />
    </Airlock>
  );
}
