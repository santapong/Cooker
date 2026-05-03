import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getUserManager } from './OIDCProvider';
import { useTheme } from '../theme/ThemeProvider';
import { Btn } from '../components/ui/atoms';
import { CookerMark } from '../components/ui/Icon';

export default function Callback() {
  const t = useTheme();
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
    <div
      style={{
        minHeight: '100vh',
        background: t.bg,
        color: t.text,
        display: 'grid',
        placeItems: 'center',
        padding: 40,
      }}
    >
      <div
        style={{
          textAlign: 'center',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: 18,
        }}
      >
        <CookerMark color={t.accent} size={40} />
        {error ? (
          <>
            <h2
              style={{
                fontFamily: t.serif,
                fontSize: 28,
                fontWeight: 500,
                color: t.text,
                margin: 0,
                letterSpacing: -0.4,
              }}
            >
              Sign-in failed.
            </h2>
            <p style={{ color: t.bad, fontFamily: t.mono, fontSize: 12.5, maxWidth: 480 }}>
              {error}
            </p>
            <Btn kind="primary" onClick={() => navigate('/', { replace: true })}>
              Back to sign in
            </Btn>
          </>
        ) : (
          <p
            style={{
              fontFamily: t.serif,
              fontSize: 24,
              fontWeight: 500,
              color: t.textSoft,
              margin: 0,
              letterSpacing: -0.3,
            }}
          >
            Completing sign-in…
          </p>
        )}
      </div>
    </div>
  );
}
