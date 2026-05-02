import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getUserManager } from './OIDCProvider';

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
      .then(() => {
        navigate('/', { replace: true });
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      });
  }, [navigate]);

  if (error) {
    return (
      <div style={{ padding: 40, textAlign: 'center', color: '#f1f5f9' }}>
        <h2>Sign-in failed</h2>
        <p style={{ color: '#94a3b8' }}>{error}</p>
      </div>
    );
  }

  return (
    <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>
      Completing sign-in…
    </div>
  );
}
