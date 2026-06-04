import { Handle, Position, type NodeProps } from '@xyflow/react';
import { useTheme } from '../../../theme/ThemeProvider';
import { hexA } from '../../../theme/tokens';
import { PlanetOrb, planetKindOf, statusTone } from '../../ui/atoms';

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

export default function StageNode({ data, kind, selected, detailFromConfig, fallbackDetail }: Props) {
  const t = useTheme();
  const d = data as StageNodeData & { config?: Record<string, unknown> };
  const status = d.status || 'pending';
  const tone = statusTone(status);
  const isLive = tone === 'ember';
  const dim = status === 'pending' || status === 'queued';
  const ring = planetKindOf(kind) === 'approval';
  const statusColor =
    tone === 'good' ? t.good
    : tone === 'bad' ? t.bad
    : tone === 'ember' ? t.ember
    : tone === 'warn' ? t.warn
    : tone === 'cool' ? t.cool
    : t.textMute;

  const detail =
    d.detail ?? (detailFromConfig && d.config ? detailFromConfig(d.config) : undefined) ?? fallbackDetail ?? 'configure…';

  const portColor = t.violetGlow;
  const portStyle: React.CSSProperties = {
    width: 9,
    height: 9,
    background: portColor,
    border: `1.5px solid ${t.mode === 'dark' ? t.bg : '#fff'}`,
    boxShadow: `0 0 8px ${hexA(portColor, 0.8)}`,
  };

  return (
    <div
      style={{
        width: 214,
        background: t.surface,
        backdropFilter: 'blur(10px)',
        WebkitBackdropFilter: 'blur(10px)',
        border: `1px solid ${selected ? t.violet : isLive ? hexA(t.ember, 0.5) : t.line}`,
        borderRadius: 14,
        padding: '11px 13px',
        opacity: dim ? 0.62 : 1,
        boxShadow: selected
          ? `0 0 0 3px ${hexA(t.violet, 0.28)}, 0 14px 40px ${hexA('#000', t.mode === 'dark' ? 0.5 : 0.14)}`
          : isLive
            ? `0 0 26px ${hexA(t.ember, 0.3)}, 0 14px 38px ${hexA('#000', t.mode === 'dark' ? 0.5 : 0.14)}`
            : `0 12px 30px ${hexA('#000', t.mode === 'dark' ? 0.42 : 0.1)}`,
        color: t.text,
        position: 'relative',
      }}
    >
      {/* atmosphere edge glow — clipped to the rounded card in its own layer so
          the React Flow ports (which straddle the border) are NOT clipped. */}
      <div
        style={{
          position: 'absolute',
          inset: 0,
          borderRadius: 14,
          overflow: 'hidden',
          pointerEvents: 'none',
        }}
      >
        <div
          style={{
            position: 'absolute',
            left: 0,
            top: 0,
            bottom: 0,
            width: 46,
            background: `radial-gradient(ellipse at left center, ${hexA(isLive ? t.ember : t.planets[planetKindOf(kind)].glow, 0.3)} 0%, transparent 75%)`,
          }}
        />
      </div>

      <Handle type="target" position={Position.Left} style={portStyle} />

      <div style={{ display: 'flex', alignItems: 'center', gap: 11, position: 'relative' }}>
        <PlanetOrb kind={kind} size={34} status={status} ring={ring} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontFamily: t.sans,
              fontSize: 13,
              fontWeight: 600,
              color: t.text,
              lineHeight: 1.1,
              letterSpacing: -0.1,
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

      <div style={{ display: 'flex', alignItems: 'center', gap: 7, marginTop: 9, position: 'relative' }}>
        <span
          style={{
            width: 7,
            height: 7,
            borderRadius: 999,
            background: statusColor,
            flexShrink: 0,
            boxShadow: isLive ? `0 0 8px ${statusColor}` : 'none',
            animation: isLive ? 'ccPulse 1.6s ease-out infinite' : 'none',
          }}
        />
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 10,
            letterSpacing: 0.7,
            textTransform: 'uppercase',
            color: isLive ? t.ember : t.textMute,
          }}
        >
          {d.duration || status}
        </span>
        {isLive && (
          <div
            style={{
              flex: 1,
              height: 3,
              marginLeft: 4,
              borderRadius: 2,
              overflow: 'hidden',
              background: hexA(t.ember, 0.18),
            }}
          >
            <div
              style={{
                width: '55%',
                height: '100%',
                borderRadius: 2,
                background: `linear-gradient(90deg, transparent, ${t.ember}, transparent)`,
                animation: 'ccShimmer 1.4s ease-in-out infinite',
              }}
            />
          </div>
        )}
        {d.environmentId && !isLive && (
          <span
            style={{
              marginLeft: 'auto',
              fontFamily: t.mono,
              fontSize: 9.5,
              color: t.cool,
              padding: '1px 6px',
              borderRadius: 999,
              border: `1px solid ${hexA(t.cool, 0.3)}`,
              background: hexA(t.cool, 0.1),
            }}
          >
            {String(d.environmentId)}
          </span>
        )}
      </div>

      <Handle type="source" position={Position.Right} style={portStyle} />
    </div>
  );
}
