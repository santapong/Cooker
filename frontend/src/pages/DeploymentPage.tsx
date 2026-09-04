import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import RunView from '../components/run/RunView';
import { appsApi } from '../api/apps';
import type { AppModel } from '../types/app';

/**
 * An app deployment run in the porthole: the run view plus the app's
 * deployed URL / health in the HUD and, for compose services, runtime
 * status and logs in the inspector and console.
 */
export default function DeploymentPage() {
  const { appId = '', pipelineId = '', runId = '' } = useParams<{ appId: string; pipelineId: string; runId: string }>();
  const [app, setApp] = useState<AppModel | null>(null);

  useEffect(() => {
    if (!appId) return;
    let cancelled = false;
    appsApi
      .get(appId)
      .then((a) => {
        if (!cancelled) setApp(a);
      })
      .catch(() => {
        if (!cancelled) setApp(null);
      });
    return () => {
      cancelled = true;
    };
  }, [appId]);

  return (
    <RunView
      pipelineId={pipelineId}
      runId={runId}
      app={app}
      heading={(p, r) => `Porthole · ${app?.name ?? p.name} · deploy ${r.id.slice(0, 8)}`}
    />
  );
}
