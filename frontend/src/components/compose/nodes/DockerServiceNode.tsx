import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { ComposeService } from '../../../types/compose';
import { useTheme } from '../../../theme/ThemeProvider';
import { hexA, type CookerTheme } from '../../../theme/tokens';
import { PlanetOrb } from '../../ui/atoms';
import { nodeShellStyle, portHandleStyle } from '../../ui/nodeStyles';

// docker status → cosmic tone color
function serviceStatusColor(t: CookerTheme, status: string): string {
  switch (status) {
    case 'running':
      return t.good;
    case 'exited':
      return t.bad;
    case 'paused':
      return t.warn;
    default:
      return t.textMute;
  }
}

export default function DockerServiceNode({ data, selected }: NodeProps) {
  const t = useTheme();
  const service = data.service as ComposeService;
  const status = service.status || 'unknown';
  const imageLabel = service.image || (service.build ? `build: ${service.build.context}` : 'no image');
  const ports = service.ports?.length ? service.ports.join(', ') : null;

  const sc = serviceStatusColor(t, status);
  const live = status === 'running';

  return (
    <div
      style={{
        ...nodeShellStyle(t, { selected, live }),
        padding: '11px 13px',
        minWidth: 200,
        cursor: 'pointer',
      }}
    >
      <Handle type="target" position={Position.Left} style={portHandleStyle(t)} />
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
      <Handle type="source" position={Position.Right} style={portHandleStyle(t)} />
    </div>
  );
}
