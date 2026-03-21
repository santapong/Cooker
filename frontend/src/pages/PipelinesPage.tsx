import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline } from '../types/pipeline';

export default function PipelinesPage() {
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();

  useEffect(() => {
    pipelineApi.list().then(setPipelines).catch(console.error).finally(() => setLoading(false));
  }, []);

  const createPipeline = async () => {
    const pipeline = await pipelineApi.create({
      name: 'New Pipeline',
      description: 'A new CI/CD pipeline',
    });
    navigate(`/pipelines/${pipeline.id}/edit`);
  };

  return (
    <div>
      <div style={styles.header}>
        <h2 style={styles.title}>Pipelines</h2>
        <button className="btn-primary" onClick={createPipeline}>New Pipeline</button>
      </div>

      {loading ? (
        <div style={styles.loading}>Loading...</div>
      ) : pipelines.length === 0 ? (
        <div style={styles.empty}>
          <p>No pipelines yet.</p>
          <p style={{ color: '#94a3b8', fontSize: 14 }}>
            Create your first CI/CD pipeline with the visual graph editor.
          </p>
          <button className="btn-primary" onClick={createPipeline} style={{ marginTop: 12 }}>
            Create Pipeline
          </button>
        </div>
      ) : (
        <div style={styles.grid}>
          {pipelines.map((p) => (
            <div
              key={p.id}
              style={styles.card}
              onClick={() => navigate(`/pipelines/${p.id}/edit`)}
            >
              <h3 style={styles.cardTitle}>{p.name}</h3>
              <p style={styles.cardDesc}>{p.description}</p>
              <div style={styles.cardMeta}>
                {p.stages.length} stages - Updated {new Date(p.updatedAt).toLocaleDateString()}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  header: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20 },
  title: { fontSize: 22, fontWeight: 700, color: '#f1f5f9', margin: 0 },
  loading: { color: '#94a3b8', textAlign: 'center', padding: 40 },
  empty: { textAlign: 'center', padding: 60, color: '#f1f5f9' },
  grid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 16 },
  card: {
    padding: 20,
    backgroundColor: '#1e293b',
    borderRadius: 8,
    border: '1px solid #334155',
    cursor: 'pointer',
    transition: 'border-color 0.15s',
  },
  cardTitle: { fontSize: 16, fontWeight: 600, color: '#f1f5f9', margin: '0 0 8px' },
  cardDesc: { fontSize: 13, color: '#94a3b8', margin: '0 0 12px' },
  cardMeta: { fontSize: 12, color: '#475569' },
};
