import { Handle, Position, type NodeProps } from '@xyflow/react';
import { useTheme } from '../../../theme/ThemeProvider';
import { hexA } from '../../../theme/tokens';
import { KindBadge, StatusDot, statusTone } from '../../ui/atoms';

interface StageNodeData {
  label?: string;
  status?: string;
  kind?: string;
  detail?: string;
  duration?: string;
  environmentId?: string;
}

interface Props extends NodeProps {
  kind: string;
  detailFromConfig?: (config: Record<string, unknown>) => string | undefined;
  fallbackDetail?: string;
}

export default function StageNode({ data, kind, detailFromConfig, fallbackDetail }: Props) {
  const t = useTheme();
  const d = data as StageNodeData & { config?: Record<string, unknown> };
  const status = d.status || 'pending';
  const tone = statusTone(status);
  const isLive = tone === 'ember';
  const stripe =
    tone === 'good' ? t.good
    : tone === 'bad' ? t.bad
    : tone === 'ember' ? t.ember
    : tone === 'warn' ? t.warn
    : t.line;

  const detail =
    d.detail ?? (detailFromConfig && d.config ? detailFromConfig(d.config) : undefined) ?? fallbackDetail ?? 'configure…';

  return (
    <div
      style={{
        width: 200,
        background: t.surface,
        border: `1.5px solid ${t.line}`,
        borderRadius: 10,
        padding: 10,
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
        borderLeft: `4px solid ${stripe}`,
        boxShadow: isLive
          ? `0 0 0 4px ${hexA(t.ember, 0.15)}, 0 4px 14px ${hexA('#000', 0.18)}`
          : `0 1px 0 ${hexA('#000', 0.05)}`,
        color: t.text,
      }}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{ background: t.accent, border: `1.5px solid ${t.bg}`, width: 9, height: 9 }}
      />
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <KindBadge kind={kind} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 12.5,
              fontWeight: 600,
              color: t.text,
              lineHeight: 1.1,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}
          >
            {d.label || kind}
          </div>
          <div
            style={{
              fontFamily: t.mono,
              fontSize: 10,
              color: t.textMute,
              marginTop: 3,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}
          >
            {detail}
          </div>
        </div>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 2 }}>
        <StatusDot tone={tone} pulse={isLive} />
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 10.5,
            color: isLive ? t.ember : t.textMute,
            textTransform: 'uppercase',
            letterSpacing: 0.6,
          }}
        >
          {d.duration || status}
        </span>
        {isLive && (
          <div
            style={{
              flex: 1,
              height: 3,
              background: t.line,
              borderRadius: 2,
              overflow: 'hidden',
              marginLeft: 6,
            }}
          >
            <div
              style={{
                width: '62%',
                height: '100%',
                background: t.ember,
                animation: 'cookerShimmer 1.4s ease-in-out infinite',
              }}
            />
          </div>
        )}
        {d.environmentId && (
          <span
            style={{
              fontFamily: t.mono,
              fontSize: 10,
              color: t.cool,
              marginLeft: 'auto',
            }}
          >
            {String(d.environmentId)}
          </span>
        )}
      </div>
      <Handle
        type="source"
        position={Position.Right}
        style={{ background: t.accent, border: `1.5px solid ${t.bg}`, width: 9, height: 9 }}
      />
    </div>
  );
}
