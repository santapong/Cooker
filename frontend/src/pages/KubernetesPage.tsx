import { useEffect } from 'react';
import { useKubernetesStore } from '../stores/kubernetesStore';
import { useTheme } from '../theme/ThemeProvider';
import { Btn, Card, HealthBar, PageHeader, Pill, Select, StatusDot, statusTone } from '../components/ui/atoms';
import { DataTable } from '../components/ui/DataTable';

export default function KubernetesPage() {
  const t = useTheme();
  const { namespaces, workloads, selectedNamespace, loading, fetchNamespaces, fetchWorkloads, setNamespace } =
    useKubernetesStore();

  useEffect(() => {
    fetchNamespaces();
    fetchWorkloads();
  }, [fetchNamespaces, fetchWorkloads]);

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="cluster view"
        title="Workloads"
        subtitle="Live view of Deployments / StatefulSets / DaemonSets across the namespaces this cluster exposes."
        actions={
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span
              style={{
                fontFamily: t.mono,
                fontSize: 11,
                letterSpacing: 1,
                textTransform: 'uppercase',
                color: t.textMute,
              }}
            >
              Namespace
            </span>
            <Select
              value={selectedNamespace}
              onChange={(e) => setNamespace(e.target.value)}
              style={{ width: 220 }}
            >
              <option value="">All namespaces</option>
              {namespaces.map((ns) => (
                <option key={ns.name} value={ns.name}>
                  {ns.name}
                </option>
              ))}
            </Select>
          </div>
        }
      />

      <Card pad={0}>
        <DataTable
          rows={workloads}
          rowKey={(w) => `${w.namespace}-${w.kind}-${w.name}`}
          empty={loading ? 'Loading…' : 'No workloads found in the selected namespace.'}
          columns={[
            {
              key: 'name',
              header: 'Workload',
              render: (w) => {
                const tone = w.ready === w.replicas && w.replicas > 0 ? 'good' : statusTone(w.status);
                return (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <StatusDot tone={tone} pulse={tone === 'ember'} />
                    <div>
                      <div style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>
                        {w.name}
                      </div>
                      <div style={{ fontSize: 11, color: t.textMute, marginTop: 2 }}>
                        {w.namespace}
                      </div>
                    </div>
                  </div>
                );
              },
            },
            {
              key: 'kind',
              header: 'Kind',
              width: '140px',
              render: (w) => <Pill>{w.kind}</Pill>,
            },
            {
              key: 'ready',
              header: 'Ready',
              width: '160px',
              render: (w) => (
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <HealthBar value={w.replicas > 0 ? (w.ready / w.replicas) * 100 : 0} />
                  <span style={{ fontFamily: t.mono, fontSize: 11, color: t.textSoft }}>
                    {w.ready}/{w.replicas}
                  </span>
                </div>
              ),
            },
            {
              key: 'image',
              header: 'Image',
              render: (w) => (
                <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                  {w.image}
                </span>
              ),
            },
            {
              key: 'actions',
              header: '',
              width: '180px',
              align: 'right',
              render: () => (
                <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
                  <Btn kind="ghost">Scale</Btn>
                  <Btn kind="secondary">Restart</Btn>
                </div>
              ),
            },
          ]}
        />
      </Card>
    </div>
  );
}
