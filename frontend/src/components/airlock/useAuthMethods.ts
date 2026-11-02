import { useCallback, useEffect, useState } from 'react';
import { authApi, type AuthMethods } from '../../api/auth';

/** Only the server's discovery response decides which real sign-in methods appear. */
export default function useAuthMethods() {
  const [methods, setMethods] = useState<AuthMethods | null>(null);
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    setError(false);
    setMethods(null);
    authApi.methods().then((result) => {
      if (active) setMethods(result);
    }).catch(() => {
      if (active) setError(true);
    });
    return () => { active = false; };
  }, [attempt]);
  const retry = useCallback(() => setAttempt((value) => value + 1), []);
  return { methods, discoveryError: error, retry };
}
