import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline } from '../types/pipeline';
import { useTheme } from '../theme/ThemeProvider';
import { Btn, Card, EmptyState, PageHeader, Pill } from '../components/ui/atoms';
import { ConstellationThumb } from '../components/ui/ConstellationThumb';
import { hexA } from '../theme/tokens';
import { SkeletonStack } from '../components/Skeleton';

export default function PipelinesPage() {
  const t = useTheme();
  const navigate = useNavigate();
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    pipelineApi
      .list()
      .then(setPipelines)
      .catch(() => setPipelines([]))
      .finally(() => setLoading(false));
  }, []);

  const createPipeline = async () => {
    const pipeline = await pipelineApi.create({
      name: 'New Pipeline',
      description: 'A new CI/CD pipeline',
    });
    navigate(`/pipelines/${pipeline.id}/edit`);
  };

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow={`${pipelines.length} pipelines`}
        title="Pipelines"
        subtitle="Visual graph editor for build · ship · run. Drag nodes onto the canvas, wire them up, watch each step run live."
        actions={
          <>
            <Btn kind="ghost">Templates</Btn>
            <Btn kind="primary" icon="plus" onClick={createPipeline}>
              New pipeline
            </Btn>
          </>
        }
      />

      {loading ? (
        <Card>
          <SkeletonStack rows={4} />
        </Card>
      ) : pipelines.length === 0 ? (
        // Empty-state CTA — Indie step 2 (W11-A3, PR #50).
        <EmptyState
          title="No pipelines yet."
          body="Draw your CI/CD graph visually — drag nodes, wire steps, watch each run stream live."
          action={
            <div style={{ display: 'flex', gap: 10, justifyContent: 'center', flexWrap: 'wrap' }}>
              <Btn kind="primary" icon="plus" onClick={createPipeline}>
                Create your first Pipeline
              </Btn>
              <a
                href="https://github.com/santapong/Cooker/blob/main/docs/user-guide/README.md"
                target="_blank"
                rel="noopener noreferrer"
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '8px 16px',
                  border: `1px solid currentColor`,
                  borderRadius: 7,
                  fontSize: 13.5,
                  color: 'inherit',
                  textDecoration: 'none',
                  opacity: 0.7,
                }}
              >
                Read the user guide ↗
              </a>
            </div>
          }
        />
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
            gap: 16,
          }}
        >
          {pipelines.map((p) => (
            <Card
              key={p.id}
              pad={20}
              style={{ cursor: 'pointer', transition: 'border-color .15s', position: 'relative', overflow: 'hidden' }}
              onClick={() => navigate(`/pipelines/${p.id}/edit`)}
            >
              {/* nebula corner glow */}
              <div
                style={{
                  position: 'absolute',
                  top: -40,
                  right: -40,
                  width: 140,
                  height: 140,
                  borderRadius: 999,
                  background: `radial-gradient(circle, ${hexA(t.violetGlow, 0.18)} 0%, transparent 70%)`,
                  pointerEvents: 'none',
                }}
              />
              <div
                style={{
                  position: 'relative',
                  display: 'flex',
                  alignItems: 'baseline',
                  justifyContent: 'space-between',
                  marginBottom: 6,
                  gap: 10,
                }}
              >
                <h3
                  style={{
                    fontFamily: t.display,
                    fontSize: 19,
                    fontWeight: 600,
                    color: t.text,
                    margin: 0,
                    letterSpacing: -0.4,
                  }}
                >
                  {p.name}
                </h3>
                <Pill tone="accent">{p.stages.length} planets</Pill>
              </div>
              <div style={{ position: 'relative', margin: '6px -4px 8px' }}>
                <ConstellationThumb kinds={p.stages.map((s) => s.type)} />
              </div>
              <p style={{ position: 'relative', fontSize: 13, color: t.textSoft, margin: '4px 0 14px', lineHeight: 1.5 }}>
                {p.description || 'No description.'}
              </p>
              <div
                style={{
                  position: 'relative',
                  display: 'flex',
                  alignItems: 'center',
                  fontFamily: t.mono,
                  fontSize: 11,
                  color: t.textMute,
                }}
              >
                updated {new Date(p.updatedAt).toLocaleDateString()}
                <span style={{ flex: 1 }} />
                <span style={{ color: t.violet }}>open map →</span>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
