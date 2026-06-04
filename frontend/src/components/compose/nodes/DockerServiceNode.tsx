import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { ComposeService } from '../../../types/compose';
import { useTheme } from '../../../theme/ThemeProvider';
import { hexA } from '../../../theme/tokens';
import { PlanetOrb } from '../../ui/atoms';

export default function DockerServiceNode({ data, selected }: NodeProps) {
  const t = useTheme();
  const service = data.service as ComposeService;
  const status = service.status || 'unknown';
  const imageLabel = service.image || (service.build ? `build: ${service.build.context}` : 'no image');
  const ports = service.ports?.length ? service.ports.join(', ') : null;

  const statusColors: Record<string, string> = {
    running: t.good,
    exited: t.bad,
    paused: t.warn,
    unknown: t.textMute,
  };
  const sc = statusColors[status] || t.textMute;
  const live = status === 'running';

  const portColor = t.violetGlow;
  const handleStyle: React.CSSProperties = {
    width: 9,
    height: 9,
    background: portColor,
    border: `1.5px solid ${t.mode === 'dark' ? t.bg : '#fff'}`,
    boxShadow: `0 0 8px ${hexA(portColor, 0.8)}`,
  };

  return (
    <div
      style={{
        padding: '11px 13px',
        borderRadius: 14,
        minWidth: 200,
        background: t.surface,
        backdropFilter: 'blur(10px)',
        WebkitBackdropFilter: 'blur(10px)',
        border: `1px solid ${selected ? t.violet : live ? hexA(t.ember, 0.5) : t.line}`,
        color: t.text,
        cursor: 'pointer',
        boxShadow: live
          ? `0 0 22px ${hexA(t.ember, 0.28)}, 0 12px 30px ${hexA('#000', t.mode === 'dark' ? 0.42 : 0.1)}`
          : `0 12px 30px ${hexA('#000', t.mode === 'dark' ? 0.42 : 0.1)}`,
      }}
    >
      <Handle type="target" position={Position.Left} style={handleStyle} />
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <PlanetOrb kind="custom" size={28} status={status} />
        <span style={{ fontSize: 13, fontWeight: 600, flex: 1, letterSpacing: -0.1 }}>
          {data.label as string}
        </span>
        <span
          style={{
            width: 8,
            height: 8,
            borderRadius: 999,
            background: sc,
            boxShadow: live ? `0 0 8px ${sc}` : 'none',
            animation: live ? 'ccPulse 1.6s ease-out infinite' : 'none',
          }}
        />
      </div>
      <div style={{ fontFamily: t.mono, fontSize: 11, color: t.textMute, marginTop: 6 }}>{imageLabel}</div>
      {ports && (
        <div
          style={{
            fontFamily: t.mono,
            fontSize: 10,
            color: t.cyan,
            marginTop: 6,
            padding: '2px 7px',
            background: hexA(t.cyan, 0.1),
            border: `1px solid ${hexA(t.cyan, 0.3)}`,
            borderRadius: 999,
            display: 'inline-block',
          }}
        >
          {ports}
        </div>
      )}
      <Handle type="source" position={Position.Right} style={handleStyle} />
    </div>
  );
}
