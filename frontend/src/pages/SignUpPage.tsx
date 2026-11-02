import { useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '../auth/OIDCProvider';
import { isOIDCSignInConfigured, type SocialProvider } from '../auth/socialSignIn';
import SocialSignIn from '../components/airlock/SocialSignIn';
import Airlock from '../components/airlock/Airlock';
import AuthHelp from '../components/airlock/AuthHelp';
import PasswordField from '../components/airlock/PasswordField';
import useAuthMethods from '../components/airlock/useAuthMethods';
import { Actions, Field, FormError, TextInput } from '../components/ui/form';

const MIN_PASSWORD = 12;

export default function SignUpPage() {
  const navigate = useNavigate();
  const { signupLocal, signinSocial, isAuthenticated } = useAuth();
  const { methods, discoveryError, retry } = useAuthMethods();
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (isAuthenticated) navigate('/', { replace: true });
  }, [isAuthenticated, navigate]);

  const tooShort = password.length > 0 && password.length < MIN_PASSWORD;
  const mismatch = confirm.length > 0 && confirm !== password;
  const canSubmit = email.length > 0 && password.length >= MIN_PASSWORD && confirm === password && !busy;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setBusy(true);
    setError(null);
    try {
      await signupLocal(email, password, name || undefined);
      navigate('/', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  };

  const allowed = methods?.local.enabled && methods.local.allowSignup;

  const social = async (provider: SocialProvider) => {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await signinSocial(provider, '/');
    } catch {
      setError('We could not start that sign-in. Try again or use another available method.');
      setBusy(false);
    }
  };

  return (
    <Airlock full title="Create an account" description="Choose an available method for this Cooker workspace." seed={5}>
      {!methods && !discoveryError && <p className="auth-status" role="status">Checking account availability…</p>}
      {discoveryError && (
        <div className="auth-status" role="alert">
          <p>We could not check whether sign-up is available. Check your connection and try again.</p>
          <button type="button" className="hud-btn btn-block" onClick={retry}>Try again</button>
        </div>
      )}
      {methods && <SocialSignIn methods={methods} oidcReady={isOIDCSignInConfigured()} busy={busy} onSignIn={(provider) => { void social(provider); }} />}
      <FormError>{error}</FormError>
      {methods && !allowed && (
        <p className="auth-status" role="status">
          Local sign-up is not open on this server. Contact your Cooker administrator for an account, or use an available sign-in method.
        </p>
      )}
      {allowed && (
        <>
        <span className="airlock-or">or create a local account</span>
        <form onSubmit={submit} aria-busy={busy}>
          <Field label="Name (optional)">
            <TextInput autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} autoFocus disabled={busy} />
          </Field>
          <Field label="Email">
            <TextInput type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required disabled={busy} />
          </Field>
          <PasswordField label="Password" hint={`Use at least ${MIN_PASSWORD} characters.`}
            autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)}
            required minLength={MIN_PASSWORD} aria-invalid={tooShort || undefined} disabled={busy} />
          <PasswordField label="Confirm password" hint={mismatch ? 'Passwords do not match.' : undefined}
            autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)}
            required aria-invalid={mismatch || undefined} disabled={busy} />
          <Actions>
            <button type="submit" className="hud-btn hud-btn-primary btn-block" disabled={!canSubmit}>
              {busy ? 'Creating…' : 'Create account'}
            </button>
          </Actions>
        </form>
        </>
      )}
      <div className="airlock-foot">
        <Link to="/signin">Already have an account? Sign in</Link>
      </div>
      <AuthHelp />
    </Airlock>
  );
}
