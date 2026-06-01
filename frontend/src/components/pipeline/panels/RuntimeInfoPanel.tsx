import { useEffect, useState } from 'react';
import { useTheme } from '../../../theme/ThemeProvider';
import { hexA } from '../../../theme/tokens';
import { StatusDot, statusTone } from '../../ui/atoms';
import { useRuntimeLogs } from '../../../hooks/useRuntimeLogs';
import { runtimeApi, type ServiceRuntimeStatus } from '../../../api/pipelines';

interface Props {
  appId: string;
  // serviceName is the compose service (from the selected node).
  serviceName: string;
  // resources is the configured per-service limit, shown verbatim.
  resources?: { memory?: string; cpus?: string };
  onClose: () => void;
}

// RuntimeInfoPanel is the deployment-view right drawer: it shows the
// live container/pod state for the selected service plus a tailing log
// viewer. Reuses useRuntimeLogs (WS) + the status atoms.
export default function RuntimeInfoPanel({ appId, serviceName, resources, onClose }: Props) {
  const t = useTheme();
  const [status, setStatus] = useState<ServiceRuntimeStatus | null>(null);
  const { lines, connected, truncated } = useRuntimeLogs({ appId, serviceId: serviceName, enabled: true });

  // Poll the runtime status snapshot every 5s while the panel is open.
  useEffect(() => {
    let cancelled = false;
    const load = () => {
      runtimeApi
        .serviceStatus(appId, serviceName)
        .then((s) => {
          if (!cancelled) setStatus(s);
        })
        .catch(() => {
          /* transient; keep last */
        });
    };
    load();
    const id = setInterval(load, 5000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [appId, serviceName]);

  const tone = statusTone(status?.healthy ? 'success' : status?.state === 'running' ? 'running' : 'pending');

  const row = (label: string, value: string | undefined) =>
    value ? (
      <div style={{ display: 'flex', gap: 8, fontSize: 11.5 }}>
        <span style={{ color: t.textMute, minWidth: 88 }}>{label}</span>
        <span style={{ fontFamily: t.mono, color: t.textSoft, wordBreak: 'break-all' }}>{value}</span>
      </div>
    ) : null;

  return (
    <div
      style={{
        width: 420,
        borderLeft: `1px solid ${t.line}`,
        background: t.surface,
        display: 'flex',
        flexDirection: 'column',
        minHeight: 0,
      }}
    >
      <div style={{ padding: '12px 14px', borderBottom: `1px solid ${t.line}`, display: 'flex', alignItems: 'center', gap: 8 }}>
        <StatusDot tone={tone} pulse={status?.state === 'running'} />
        <span style={{ fontFamily: t.serif, fontSize: 15, color: t.text }}>{serviceName}</span>
        <div style={{ flex: 1 }} />
        <button
          onClick={onClose}
          style={{ background: 'transparent', border: 'none', color: t.textMute, cursor: 'pointer', fontSize: 18 }}
          aria-label="Close"
        >
          ×
        </button>
      </div>

      <div style={{ padding: '12px 14px', display: 'flex', flexDirection: 'column', gap: 6, borderBottom: `1px solid ${t.line}` }}>
        {row('runtime', status?.runtime)}
        {row('ref', status?.ref)}
        {row('state', status?.state)}
        {row('image', status?.image)}
        {row('memory', resources?.memory)}
        {row('cpus', resources?.cpus)}
        {status?.message && row('note', status.message)}
      </div>

      <div style={{ padding: '8px 14px', fontSize: 10.5, color: t.textMute, fontFamily: t.mono, textTransform: 'uppercase', letterSpacing: 0.5, display: 'flex', alignItems: 'center', gap: 8 }}>
        <StatusDot tone={connected ? 'good' : 'neutral'} />
        live logs {connected ? '· connected' : '· connecting…'}
        {truncated && <span style={{ color: t.warn }}>· truncated</span>}
      </div>

      <div
        style={{
          flex: 1,
          overflow: 'auto',
          background: hexA(t.text, 0.03),
          fontFamily: t.mono,
          fontSize: 11.5,
          lineHeight: 1.5,
          padding: '8px 14px',
          minHeight: 0,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-all',
        }}
      >
        {lines.length === 0 ? (
          <span style={{ color: t.textMute }}>waiting for log output…</span>
        ) : (
          lines.map((l, i) => (
            <div key={i} style={{ color: t.textSoft }}>
              {l}
            </div>
          ))
        )}
      </div>
    </div>
  );
}
