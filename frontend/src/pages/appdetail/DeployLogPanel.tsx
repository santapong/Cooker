import type { RefObject } from 'react';
import type { AppDeployResponse } from '../../types/app';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Card, Pill, SectionLabel, statusTone } from '../../components/ui/atoms';

// DeployLogPanel is the right-hand column of AppDetailPage: the live
// build/deploy log stream plus the last-deploy summary. Split out of
// DeploymentsPanel (rather than merged into it) because it occupies the
// second column of the page's 320px/1fr grid — a different DOM parent than
// the sidebar cards DeploymentsPanel owns — so it needs its own call site
// in AppDetailPage.tsx to avoid reshaping the grid.
export default function DeployLogPanel({
  logs,
  logRef,
  lastDeploy,
  deploying,
}: {
  logs: string;
  logRef: RefObject<HTMLPreElement>;
  lastDeploy: AppDeployResponse | null;
  deploying: boolean;
}) {
  const t = useTheme();
  return (
    <Card pad={0}>
      <div
        style={{
          padding: '14px 18px',
          borderBottom: `1px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 12,
        }}
      >
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          Build & deploy logs
        </span>
        {lastDeploy && (
          <Pill tone="cool">run {lastDeploy.runId.slice(0, 8)}</Pill>
        )}
        <div style={{ flex: 1 }} />
        {deploying && <Pill tone="ember">streaming</Pill>}
      </div>

      {/* Last deploy summary — Indie step 6 (W11-A2, PR #50).
          Visible once a deploy has been triggered this session.
          url is optional (docker-host targets may not expose an ingress URL). */}
      {lastDeploy && (
        <div
          style={{
            padding: '12px 18px',
            borderBottom: `1px solid ${hexA(t.line, 0.5)}`,
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
          }}
        >
          <SectionLabel>Last deploy</SectionLabel>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'max-content 1fr',
              gap: '6px 14px',
              fontSize: 12.5,
              alignItems: 'center',
            }}
          >
            <span style={{ color: t.textMute }}>Status</span>
            <span>
              <Pill tone={statusTone(lastDeploy.status)}>{lastDeploy.status}</Pill>
            </span>
            <span style={{ color: t.textMute }}>Run</span>
            <span style={{ fontFamily: t.mono, fontSize: 12, color: t.text }}>
              {lastDeploy.runId.slice(0, 8)}
            </span>
            {lastDeploy.url && (
              <>
                <span style={{ color: t.textMute }}>URL</span>
                <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span
                    style={{
                      fontFamily: t.mono,
                      fontSize: 11.5,
                      color: t.text,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                      maxWidth: 300,
                    }}
                  >
                    {lastDeploy.url}
                  </span>
                  <a
                    href={lastDeploy.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{
                      fontFamily: t.mono,
                      fontSize: 11.5,
                      color: t.accent,
                      textDecoration: 'none',
                      whiteSpace: 'nowrap',
                      border: `1px solid ${hexA(t.accent, 0.4)}`,
                      borderRadius: 5,
                      padding: '2px 8px',
                    }}
                    aria-label={`Visit deployed app at ${lastDeploy.url}`}
                  >
                    Visit ↗
                  </a>
                </span>
              </>
            )}
          </div>
        </div>
      )}

      <pre
        ref={logRef}
        style={{
          background: t.bg,
          color: t.text,
          fontFamily: t.mono,
          fontSize: 12,
          padding: 16,
          margin: 0,
          maxHeight: 480,
          minHeight: 280,
          overflow: 'auto',
          whiteSpace: 'pre-wrap',
          borderTop: `1px solid ${hexA(t.line, 0.4)}`,
        }}
      >
        {logs || 'Click Deploy to start. Build logs stream here in real time.'}
      </pre>
    </Card>
  );
}
