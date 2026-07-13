// Stub — the previous design was removed for a full redesign (Phase 2).
// Rebuild this screen fresh; plumbing (api/, stores/, hooks/) is intact.
// Kept functional beyond a bare placeholder: it loads the app and shows
// the "Open app" link once a deploy has populated deployedURL (Phase 5).
import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppModel } from '../types/app';

export default function AppDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [app, setApp] = useState<AppModel | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!id) return;
    appsApi
      .get(id)
      .then(setApp)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)));
  }, [id]);

  return (
    <main style={{ padding: 24 }}>
      <h1>{app ? app.name : 'App'}</h1>
      {error && <p role="alert">{error}</p>}
      {app?.deployedURL && (
        <p>
          <a href={app.deployedURL} target="_blank" rel="noreferrer noopener">
            Open app ↗ {app.deployedURL}
          </a>
        </p>
      )}
      <p>This screen is a placeholder pending redesign.</p>
    </main>
  );
}
