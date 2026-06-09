import { usePipelineStore } from '../../../stores/pipelineStore';
import { useTheme } from '../../../theme/ThemeProvider';
import { Btn, Field, KindBadge, Pill, SectionLabel, Input, Label, Select, Toggle } from '../../ui/atoms';
import { Icon } from '../../ui/Icon';
import type { CacheSpec } from '../../../types/pipeline';

function numOrUndef(v: string): number | undefined {
  if (v === '') return undefined;
  const n = Number(v);
  return Number.isFinite(n) ? n : undefined;
}

export default function NodeConfigPanel() {
  const t = useTheme();
  const pipeline = usePipelineStore((s) => s.pipeline);
  const selectedNodeId = usePipelineStore((s) => s.selectedNodeId);
  const updateStageConfig = usePipelineStore((s) => s.updateStageConfig);
  const removeStage = usePipelineStore((s) => s.removeStage);
  const setSelectedNode = usePipelineStore((s) => s.setSelectedNode);

  if (!pipeline || !selectedNodeId) return null;

  const stage = pipeline.stages.find((s) => s.id === selectedNodeId);
  if (!stage) return null;

  const config = stage.config as Record<string, string | undefined>;
  const retry = stage.config.retry;
  const cache = stage.config.cache;

  return (
    <aside
      style={{
        width: 320,
        background: t.surface,
        borderLeft: `1px solid ${t.line}`,
        display: 'flex',
        flexDirection: 'column',
        flexShrink: 0,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          padding: '16px 18px',
          borderBottom: `1px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}
      >
        <KindBadge kind={stage.type} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: t.text }}>{stage.name}</div>
          <div style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
            step.id = {stage.id.slice(0, 12)}
          </div>
        </div>
        <Pill tone="neutral">{stage.type}</Pill>
        <button
          onClick={() => setSelectedNode(null)}
          style={{
            background: 'transparent',
            border: `1px solid ${t.line}`,
            color: t.textSoft,
            width: 28,
            height: 28,
            borderRadius: 6,
            display: 'grid',
            placeItems: 'center',
            cursor: 'pointer',
          }}
        >
          <Icon name="close" size={12} />
        </button>
      </div>

      <div
        style={{
          padding: '16px 18px',
          display: 'flex',
          flexDirection: 'column',
          gap: 14,
          fontSize: 12.5,
          overflow: 'auto',
          flex: 1,
        }}
      >
        <Field label="Step name" mono={stage.name} />

        {stage.type === 'build' && (
          <>
            <div>
              <Label>Dockerfile</Label>
              <Input
                value={config.dockerfile || ''}
                onChange={(e) => updateStageConfig(stage.id, { dockerfile: e.target.value })}
                placeholder="./Dockerfile"
              />
            </div>
            <div>
              <Label>Build context</Label>
              <Input
                value={config.context || ''}
                onChange={(e) => updateStageConfig(stage.id, { context: e.target.value })}
                placeholder="."
              />
            </div>
            <div style={{ marginTop: 4 }}>
              <SectionLabel>Layer cache</SectionLabel>
            </div>
            <div>
              <Label>Mode</Label>
              <Select
                value={cache?.mode || ''}
                onChange={(e) =>
                  updateStageConfig(stage.id, {
                    cache: e.target.value
                      ? { ...cache, mode: e.target.value as CacheSpec['mode'] }
                      : undefined,
                  })
                }
              >
                <option value="">Off (cold builds)</option>
                <option value="registry">Registry</option>
                <option value="oci">Registry (OCI media types)</option>
              </Select>
            </div>
            {(cache?.mode === 'registry' || cache?.mode === 'oci') && (
              <div>
                <Label>Cache image ref</Label>
                <Input
                  value={cache?.ref || ''}
                  onChange={(e) =>
                    updateStageConfig(stage.id, { cache: { ...cache, ref: e.target.value } })
                  }
                  placeholder="registry.example.com/org/app:buildcache"
                />
              </div>
            )}
          </>
        )}

        {stage.type === 'test' && (
          <div>
            <Label>Test image</Label>
            <Input
              value={config.image || ''}
              onChange={(e) => updateStageConfig(stage.id, { image: e.target.value })}
              placeholder="node:20-alpine"
            />
          </div>
        )}

        {stage.type === 'push' && (
          <>
            <div>
              <Label>Registry</Label>
              <Input
                value={config.registry || ''}
                onChange={(e) => updateStageConfig(stage.id, { registry: e.target.value })}
                placeholder="ghcr.io"
              />
            </div>
            <div>
              <Label>Repository</Label>
              <Input
                value={config.repository || ''}
                onChange={(e) => updateStageConfig(stage.id, { repository: e.target.value })}
                placeholder="org/image"
              />
            </div>
          </>
        )}

        {stage.type === 'deploy' && (
          <>
            <div>
              <Label>Namespace</Label>
              <Input
                value={config.namespace || ''}
                onChange={(e) => updateStageConfig(stage.id, { namespace: e.target.value })}
                placeholder="default"
              />
            </div>
            <div>
              <Label>Manifest path</Label>
              <Input
                value={config.manifestPath || ''}
                onChange={(e) => updateStageConfig(stage.id, { manifestPath: e.target.value })}
                placeholder="k8s/deployment.yaml"
              />
            </div>
          </>
        )}

        {stage.type === 'custom' && (
          <div>
            <Label>Script</Label>
            <Input
              value={config.script || ''}
              onChange={(e) => updateStageConfig(stage.id, { script: e.target.value })}
              placeholder="echo hello"
            />
          </div>
        )}

        {(stage.type === 'build' || stage.type === 'push' || stage.type === 'deploy') && (
          <>
            <div style={{ marginTop: 4 }}>
              <SectionLabel>Retry</SectionLabel>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
              <div>
                <Label>Max attempts</Label>
                <Input
                  type="number"
                  min={0}
                  max={10}
                  value={retry?.maxAttempts ?? ''}
                  onChange={(e) =>
                    updateStageConfig(stage.id, {
                      retry: { ...retry, maxAttempts: numOrUndef(e.target.value) },
                    })
                  }
                  placeholder="1 (no retry)"
                />
              </div>
              <div>
                <Label>First delay (ms)</Label>
                <Input
                  type="number"
                  min={0}
                  max={60000}
                  value={retry?.initialMs ?? ''}
                  onChange={(e) =>
                    updateStageConfig(stage.id, {
                      retry: { ...retry, initialMs: numOrUndef(e.target.value) },
                    })
                  }
                  placeholder="1000"
                />
              </div>
              <div>
                <Label>Max delay (ms)</Label>
                <Input
                  type="number"
                  min={0}
                  max={300000}
                  value={retry?.maxMs ?? ''}
                  onChange={(e) =>
                    updateStageConfig(stage.id, {
                      retry: { ...retry, maxMs: numOrUndef(e.target.value) },
                    })
                  }
                  placeholder="15000"
                />
              </div>
              <div style={{ display: 'flex', alignItems: 'flex-end', paddingBottom: 6 }}>
                <Toggle
                  on={retry?.exponential !== false}
                  onClick={() =>
                    updateStageConfig(stage.id, {
                      retry: { ...retry, exponential: retry?.exponential === false },
                    })
                  }
                  label="exponential"
                />
              </div>
            </div>
          </>
        )}

        <div style={{ marginTop: 4 }}>
          <SectionLabel>Promotion</SectionLabel>
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <Pill tone="good">auto from previous</Pill>
          <Pill tone="warn">manual to next</Pill>
        </div>

        <div style={{ flex: 1 }} />

        <Btn kind="danger" onClick={() => removeStage(stage.id)}>
          Remove step
        </Btn>
      </div>
    </aside>
  );
}
