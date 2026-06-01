import type { NodeProps } from '@xyflow/react';
import { useTheme } from '../../../theme/ThemeProvider';
import { hexA } from '../../../theme/tokens';

// GroupNodeData carries the group label and an aggregate tone derived
// from the member stages' statuses (see deploymentLayout). The node is
// a non-interactive container; member stage nodes are parented to it
// via parentId so they render inside the box.
export interface GroupNodeData {
  label?: string;
  tone?: 'idle' | 'good' | 'bad' | 'ember' | 'warn';
  count?: number;
}

export default function GroupNode({ data }: NodeProps) {
  const t = useTheme();
  const d = data as GroupNodeData;
  const tone = d.tone || 'idle';
  const accent =
    tone === 'good' ? t.good
    : tone === 'bad' ? t.bad
    : tone === 'ember' ? t.ember
    : tone === 'warn' ? t.warn
    : t.line;

  return (
    <div
      style={{
        width: '100%',
        height: '100%',
        background: hexA(accent, 0.05),
        border: `1.5px dashed ${hexA(accent, 0.55)}`,
        borderRadius: 14,
        boxSizing: 'border-box',
      }}
    >
      <div
        style={{
          position: 'absolute',
          top: -11,
          left: 14,
          background: t.bg,
          padding: '1px 10px',
          borderRadius: 8,
          border: `1px solid ${hexA(accent, 0.55)}`,
          display: 'flex',
          alignItems: 'center',
          gap: 8,
        }}
      >
        <span
          style={{
            width: 8,
            height: 8,
            borderRadius: '50%',
            background: accent,
            display: 'inline-block',
          }}
        />
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 11,
            fontWeight: 600,
            letterSpacing: 0.4,
            color: t.textSoft,
            textTransform: 'uppercase',
          }}
        >
          {d.label || 'group'}
        </span>
        {typeof d.count === 'number' && (
          <span style={{ fontFamily: t.mono, fontSize: 10, color: t.textMute }}>
            {d.count} svc
          </span>
        )}
      </div>
    </div>
  );
}
