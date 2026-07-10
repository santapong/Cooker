import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getUserManager } from './OIDCProvider';

// Design reset (Phase 2): the OIDC redirect-callback logic is plumbing and
// stays intact; the UI is unstyled pending redesign.
export default function Callback() {
  const navigate = useNavigate();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const manager = getUserManager();
    if (!manager) {
      navigate('/', { replace: true });
      return;
    }
    manager
      .signinRedirectCallback()
      .then(() => navigate('/', { replace: true }))
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      });
  }, [navigate]);

  return (
    <div style={{ padding: 40 }}>
      {error ? (
        <>
          <h2>Sign-in failed.</h2>
          <p>{error}</p>
          <button type="button" onClick={() => navigate('/', { replace: true })}>
            Back to sign in
          </button>
        </>
      ) : (
        <p>Completing sign-in…</p>
      )}
    </div>
  );
}
