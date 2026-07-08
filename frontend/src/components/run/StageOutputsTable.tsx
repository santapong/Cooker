import { useTheme } from '../../theme/ThemeProvider';
import { Pill } from '../ui/atoms';
import type { StageRun } from '../../types/pipeline';

export default function StageOutputsTable({ stageRun }: { stageRun: StageRun | null }) {
  const t = useTheme();

  const outputs = stageRun?.outputs;
  if (!outputs) return null;

  const visibleEntries = Object.entries(outputs).filter(([k]) => !k.startsWith('_'));
  const isTruncated = '_truncated' in outputs;
  const hasInvalid = '_invalid' in outputs;

  if (visibleEntries.length === 0 && !isTruncated && !hasInvalid) return null;

  return (
    <div
      style={{
        borderTop: `1px solid ${t.line}`,
        background: t.surface,
        padding: '10px 18px',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          marginBottom: visibleEntries.length > 0 ? 8 : 0,
        }}
      >
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 11,
            letterSpacing: 1.4,
            textTransform: 'uppercase',
            color: t.textMute,
          }}
        >
          Outputs
        </span>
        <span style={{ flex: 1, height: 1, background: t.line }} />
        {isTruncated && (
          <Pill tone="warn">outputs truncated</Pill>
        )}
        {hasInvalid && (
          <Pill tone="bad">some outputs rejected</Pill>
        )}
      </div>

      {visibleEntries.length > 0 && (
        <table
          style={{
            width: '100%',
            borderCollapse: 'collapse',
            fontFamily: t.mono,
            fontSize: 11.5,
          }}
        >
          <tbody>
            {visibleEntries.map(([key, value]) => (
              <tr key={key}>
                <td
                  style={{
                    padding: '3px 12px 3px 0',
                    color: t.textMute,
                    whiteSpace: 'nowrap',
                    verticalAlign: 'top',
                    width: '1%',
                  }}
                >
                  {key}
                </td>
                <td
                  style={{
                    padding: '3px 0',
                    color: t.text,
                    wordBreak: 'break-all',
                  }}
                >
                  {/* Coerce defensively: outputs is typed Record<string,string>,
                      but a widened / non-string value (e.g. a rollout object)
                      would otherwise render as [object Object] or throw. */}
                  {String(value)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
