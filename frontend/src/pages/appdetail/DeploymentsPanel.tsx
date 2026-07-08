import type { AppCanary, AppDeployRecord } from '../../types/app';
import { useTheme } from '../../theme/ThemeProvider';
import { Btn, Card, Pill, SectionLabel, statusTone } from '../../components/ui/atoms';

// DeploymentsPanel groups the two "in-flight or past deploy" cards that sit
// in the sidebar, in original page order: the live canary rollout banner
// (while one is progressing) followed by the deploy history / one-click
// rollback list. Kept together because both are conditionally-rendered,
// contiguous siblings in the sidebar column — splitting the live-rollout
// card into ServicesPanel would require interleaving two components across
// a single flex container, which risks reordering the DOM (see
// AppDetailPage.tsx composition comment for the full rationale).
export default function DeploymentsPanel({
  activeCanary,
  canaryBusy,
  onPromoteCanary,
  onAbortCanary,
  history,
  onRollback,
}: {
  activeCanary: AppCanary | null;
  canaryBusy: boolean;
  onPromoteCanary: () => void;
  onAbortCanary: () => void;
  history: AppDeployRecord[];
  onRollback: (deployId: string, imageRef: string) => void;
}) {
  const t = useTheme();
  return (
    <>
      {/* Canary rollout status panel (OR-1). Shown only while a
          canary is in flight; Promote / Abort drive the service. */}
      {activeCanary && activeCanary.status === 'progressing' && (
        <Card accent={t.ember} style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <SectionLabel>Canary rollout</SectionLabel>
            <div style={{ flex: 1 }} />
            <Pill tone={activeCanary.healthy ? 'good' : 'bad'}>
              {activeCanary.healthy ? 'healthy' : 'unhealthy'}
            </Pill>
          </div>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'max-content 1fr',
              gap: '6px 14px',
              fontSize: 12.5,
              alignItems: 'center',
            }}
          >
            <span style={{ color: t.textMute }}>Traffic</span>
            <span style={{ fontFamily: t.mono, color: t.text }}>{activeCanary.weight}% to canary</span>
            <span style={{ color: t.textMute }}>New image</span>
            <span
              style={{
                fontFamily: t.mono,
                fontSize: 11,
                color: t.textSoft,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
              title={activeCanary.canaryImage}
            >
              {activeCanary.canaryImage.split('/').pop()}
            </span>
            <span style={{ color: t.textMute }}>Mode</span>
            <span style={{ color: t.textSoft }}>
              {activeCanary.autoPromote
                ? `auto-promote after ${activeCanary.healthWindowSeconds}s healthy`
                : 'manual (awaiting decision)'}
            </span>
          </div>
          {activeCanary.message && (
            <div style={{ fontSize: 11.5, color: t.textMute, fontStyle: 'italic' }}>
              {activeCanary.message}
            </div>
          )}
          <div style={{ display: 'flex', gap: 8, marginTop: 2 }}>
            <Btn kind="danger" onClick={onAbortCanary} disabled={canaryBusy}>
              Abort
            </Btn>
            <div style={{ flex: 1 }} />
            <Btn kind="primary" onClick={onPromoteCanary} disabled={canaryBusy}>
              {canaryBusy ? 'Working…' : 'Promote'}
            </Btn>
          </div>
        </Card>
      )}

      {/* Deploy history + one-click rollback (roadmap M3) */}
      {history.length > 0 && (
        <Card style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <SectionLabel>Deploy history</SectionLabel>
          {history.map((d, i) => (
            <div
              key={d.id}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                paddingBottom: 8,
                borderBottom: i < history.length - 1 ? `1px solid ${t.line}` : 'none',
              }}
            >
              <Pill tone={statusTone(d.status)}>{d.status}</Pill>
              {d.kind === 'rollback' && <Pill tone="cool">rollback</Pill>}
              <span
                style={{
                  fontFamily: t.mono,
                  fontSize: 10.5,
                  color: t.textSoft,
                  flex: 1,
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
                title={d.imageRef || d.runId}
              >
                {d.imageRef ? d.imageRef.split('/').pop() : `run ${d.runId.slice(0, 8)}`}
              </span>
              <span style={{ fontSize: 10, color: t.textMute, whiteSpace: 'nowrap' }}>
                {new Date(d.createdAt).toLocaleString()}
              </span>
              {i > 0 && d.status === 'success' && d.kind === 'deploy' && d.imageRef && (
                <Btn onClick={() => onRollback(d.id, d.imageRef!)}>Roll back</Btn>
              )}
            </div>
          ))}
        </Card>
      )}
    </>
  );
}
