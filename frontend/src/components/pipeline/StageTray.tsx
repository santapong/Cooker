import type { DragEvent } from 'react';
import type { StageType } from '../../types/pipeline';

export const DRAG_MIME = 'application/cooker-node';

const STAGE_TYPES: { type: StageType; label: string }[] = [
  { type: 'build', label: 'Build' },
  { type: 'test', label: 'Test' },
  { type: 'push', label: 'Push' },
  { type: 'deploy', label: 'Deploy' },
  { type: 'approval', label: 'Approval' },
  { type: 'custom', label: 'Custom' },
];

interface Props {
  onAdd: (type: StageType) => void;
}

/** Bottom-centre capsule of stage types — drag a chip onto the porthole, or click to add at centre. */
export default function StageTray({ onAdd }: Props) {
  const onDragStart = (type: StageType) => (e: DragEvent<HTMLButtonElement>) => {
    e.dataTransfer.setData(DRAG_MIME, type);
    e.dataTransfer.effectAllowed = 'move';
  };
  return (
    <div className="tray" role="toolbar" aria-label="Add stage">
      {STAGE_TYPES.map((s) => (
        <button
          key={s.type}
          type="button"
          className="chip"
          draggable
          onDragStart={onDragStart(s.type)}
          onClick={() => onAdd(s.type)}
          title={`Add a ${s.label} stage — drag onto the porthole or click`}
        >
          ＋ {s.label}
        </button>
      ))}
    </div>
  );
}
