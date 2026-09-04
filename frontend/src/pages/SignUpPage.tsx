import { useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { authApi, type AuthMethods } from '../api/auth';
import { useAuth } from '../auth/OIDCProvider';
import Airlock from '../components/airlock/Airlock';
import { Actions, Field, FormError, TextInput } from '../components/ui/form';

const MIN_PASSWORD = 12;

export default function SignUpPage() {
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
    authApi.methods().then(setMethods).catch(() => setMethods(null));
  }, []);
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

  return (
    <Airlock full title="Create an account" seed={5}>
      {methods && !allowed && (
        <p>
          Sign-up is not open on this server. <Link to="/signin">Back to sign in</Link>
        </p>
      )}
      {allowed && (
        <form onSubmit={submit}>
          <Field label="Name">
            <TextInput autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </Field>
          <Field label="Email">
            <TextInput type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          </Field>
          <Field label="Password" hint={tooShort ? `At least ${MIN_PASSWORD} characters.` : undefined}>
            <TextInput type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </Field>
          <Field label="Confirm password" hint={mismatch ? 'Passwords do not match.' : undefined}>
            <TextInput type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
          </Field>
          <FormError>{error}</FormError>
          <Actions>
            <button type="submit" className="hud-btn hud-btn-primary btn-block" disabled={!canSubmit}>
              {busy ? 'Creating…' : 'Create account'}
            </button>
          </Actions>
        </form>
      )}
      <div className="airlock-foot">
        <Link to="/signin">Already have an account? Sign in</Link>
      </div>
    </Airlock>
  );
}
