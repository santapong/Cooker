import { useEffect, useRef, useState } from 'react';
import type { User } from 'oidc-client-ts';
import { useNavigate } from 'react-router-dom';
import { getUserManager } from './OIDCProvider';
import Airlock from '../components/airlock/Airlock';
import AuthHelp from '../components/airlock/AuthHelp';
import { FormError } from '../components/ui/form';
import { safeAuthReturnPath } from './socialSignIn';
export default function Callback() {
  const navigate = useNavigate();
  const [error, setError] = useState<string | null>(null);
  const completion = useRef<Promise<User> | null>(null);

  useEffect(() => {
    const manager = getUserManager();
    if (!manager) {
      navigate('/', { replace: true });
      return;
    }
    let active = true;
    completion.current ??= manager.signinRedirectCallback();
    completion.current
      .then((user) => {
        const state = user.state as { returnTo?: unknown } | undefined;
        if (active) navigate(safeAuthReturnPath(state?.returnTo), { replace: true });
      })
      .catch(() => {
        if (active) setError('This sign-in expired or could not be verified. Please start again.');
      });
    return () => { active = false; };
  }, [navigate]);

  return (
    <Airlock full title={error ? 'Sign-in failed' : 'Completing sign-in'}
      description={error ? 'Your identity provider could not complete this sign-in.' : 'Returning you to your Cooker workspace.'}>
      {error ? (
        <>
          <FormError>{error}</FormError>
          <button type="button" className="hud-btn hud-btn-primary btn-block" onClick={() => navigate('/signin', { replace: true })}>
            Back to sign in
          </button>
        </>
      ) : (
        <p className="auth-status" role="status">Completing sign-in…</p>
      )}
      {error && <AuthHelp />}
    </Airlock>
  );
}
