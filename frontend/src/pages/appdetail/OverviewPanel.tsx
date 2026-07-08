import type { AppDriftReport, AppModel } from '../../types/app';
import { useTheme } from '../../theme/ThemeProvider';
import { Card, Field, Pill } from '../../components/ui/atoms';

// OverviewPanel is the app-identity summary card at the top of the sidebar:
// repo/branch/registry facts plus at-a-glance status pills (webhook,
// auto-deploy, canary strategy, drift). It is intentionally read-only —
// all mutation affordances (deploy strategy, webhook rotation) live in
// ServicesPanel.
export default function OverviewPanel({
  app,
  drift,
}: {
  app: AppModel;
  drift: AppDriftReport | null;
}) {
  const t = useTheme();
  return (
    <Card style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <Field label="App ID" mono={app.id} />
      <Field label="Repo" mono={`github.com/${app.githubRepo}`} />
      <Field label="Branch" mono={app.branch} />
      {app.registryRef && <Field label="Registry ref" mono={app.registryRef} />}
      {app.environmentId && <Field label="Environment" mono={app.environmentId} />}
      {/* Deployed URL surfaced from AppHealthChecker (Indie step 6, W11-A2) */}
      {app.deployedURL && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 12, color: t.textMute, minWidth: 80 }}>Deployed URL</span>
          <a
            href={app.deployedURL}
            target="_blank"
            rel="noopener noreferrer"
            style={{
              fontFamily: t.mono,
              fontSize: 11.5,
              color: t.accent,
              textDecoration: 'none',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
            aria-label={`Visit deployed app at ${app.deployedURL}`}
          >
            {app.deployedURL}
          </a>
        </div>
      )}
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 6 }}>
        {app.hasWebhook && <Pill tone="cool">webhook</Pill>}
        {app.autoDeploy && <Pill tone="good">auto-deploy</Pill>}
        {app.canary?.strategy === 'canary' && <Pill tone="ember">canary</Pill>}
        {drift?.status === 'in_sync' && <Pill tone="good">in sync</Pill>}
        {drift?.status === 'drift' && <Pill tone="warn">drift</Pill>}
      </div>
    </Card>
  );
}
