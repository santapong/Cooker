import { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline } from '../types/pipeline';
import { useTheme } from '../theme/ThemeProvider';
import { Btn, Card, EmptyState, PageHeader, Pill } from '../components/ui/atoms';
import { ConstellationThumb } from '../components/ui/ConstellationThumb';
import { hexA } from '../theme/tokens';
import { SkeletonStack } from '../components/Skeleton';
import { downloadText, pipelineYamlFilename } from '../utils/download';
import { pushToast } from '../stores/toastStore';

export default function PipelinesPage() {
  const t = useTheme();
  const navigate = useNavigate();
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [loading, setLoading] = useState(true);
  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState('');
  const [importing, setImporting] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

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

  // Pipeline-as-code export: fetch the YAML document and save it. The
  // card navigates on click, so the caller stops propagation.
  const exportPipeline = async (p: Pipeline) => {
    try {
      const yaml = await pipelineApi.exportYaml(p.id);
      downloadText(pipelineYamlFilename(p.name), yaml);
    } catch (e) {
      pushToast('error', `Export failed: ${(e as Error).message}`);
    }
  };

  // Pipeline-as-code import: POST the YAML document, then open the
  // freshly-created pipeline in the editor. The backend mints a new id
  // and runs the same DAG validation as the editor, so a malformed
  // document surfaces its validation error here.
  const importPipeline = async () => {
    if (!importText.trim()) {
      pushToast('warning', 'Paste a pipeline YAML document or choose a file first.');
      return;
    }
    setImporting(true);
    try {
      const created = await pipelineApi.importYaml(importText);
      pushToast('success', `Imported "${created.name}".`);
      setImportOpen(false);
      setImportText('');
      navigate(`/pipelines/${created.id}/edit`);
    } catch (e) {
      pushToast('error', `Import failed: ${(e as Error).message}`);
    } finally {
      setImporting(false);
    }
  };

  const onFileChosen = (file: File | undefined) => {
    if (!file) return;
    file
      .text()
      .then((text) => {
        setImportText(text);
        setImportOpen(true);
      })
      .catch(() => pushToast('error', 'Could not read that file.'));
  };

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <input
        ref={fileInputRef}
        type="file"
        accept=".yaml,.yml,application/yaml,text/yaml"
        style={{ display: 'none' }}
        onChange={(e) => {
          onFileChosen(e.target.files?.[0]);
          e.target.value = '';
        }}
      />
      <PageHeader
        eyebrow={`${pipelines.length} pipelines`}
        title="Pipelines"
        subtitle="Visual graph editor for build · ship · run. Drag nodes onto the canvas, wire them up, watch each step run live."
        actions={
          <>
            <Btn kind="ghost">Templates</Btn>
            <Btn kind="secondary" icon="arrow" onClick={() => setImportOpen((v) => !v)}>
              Import YAML
            </Btn>
            <Btn kind="primary" icon="plus" onClick={createPipeline}>
              New pipeline
            </Btn>
          </>
        }
      />

      {importOpen && (
        <Card pad={18} style={{ marginBottom: 18 }}>
          <div
            style={{
              display: 'flex',
              alignItems: 'baseline',
              justifyContent: 'space-between',
              marginBottom: 10,
            }}
          >
            <h3 style={{ fontFamily: t.display, fontSize: 16, fontWeight: 600, color: t.text, margin: 0 }}>
              Import pipeline as code
            </h3>
            <span style={{ fontFamily: t.mono, fontSize: 11, color: t.textMute }}>
              apiVersion: cooker.dev/v1
            </span>
          </div>
          <p style={{ fontSize: 12.5, color: t.textSoft, margin: '0 0 10px', lineHeight: 1.5 }}>
            Paste a pipeline YAML document (or choose a .yaml file). A new pipeline is created
            with a fresh ID — importing the same file twice yields two pipelines.
          </p>
          <textarea
            value={importText}
            onChange={(e) => setImportText(e.target.value)}
            placeholder={'apiVersion: cooker.dev/v1\nkind: Pipeline\nmetadata:\n  name: my-pipeline\nspec:\n  stages: []\n  edges: []'}
            spellCheck={false}
            style={{
              width: '100%',
              minHeight: 180,
              fontFamily: t.mono,
              fontSize: 12,
              color: t.text,
              background: t.bg,
              border: `1px solid ${t.line}`,
              borderRadius: 8,
              padding: 12,
              resize: 'vertical',
              boxSizing: 'border-box',
            }}
          />
          <div style={{ display: 'flex', gap: 8, marginTop: 10 }}>
            <Btn kind="secondary" onClick={() => fileInputRef.current?.click()}>
              Choose file…
            </Btn>
            <span style={{ flex: 1 }} />
            <Btn
              kind="ghost"
              onClick={() => {
                setImportOpen(false);
                setImportText('');
              }}
            >
              Cancel
            </Btn>
            <Btn kind="primary" onClick={importPipeline} disabled={importing}>
              {importing ? 'Importing…' : 'Import pipeline'}
            </Btn>
          </div>
        </Card>
      )}

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
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Pill tone="accent">{p.stages.length} planets</Pill>
                  {/* Export-as-code: the card navigates on click, so stop
                      propagation and download the YAML in place. */}
                  <Btn
                    kind="ghost"
                    icon="arrow"
                    style={{ padding: '5px 9px', fontSize: 11.5 }}
                    onClick={(e) => {
                      e.stopPropagation();
                      void exportPipeline(p);
                    }}
                  >
                    Export
                  </Btn>
                </div>
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
