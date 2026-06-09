import { useEffect } from 'react';
import { useKubernetesStore } from '../stores/kubernetesStore';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Card, EmptyState, HealthBar, PageHeader, Pill, Select, StatusDot, statusTone } from '../components/ui/atoms';
import { DataTable } from '../components/ui/DataTable';

function ClusterUnavailableBanner() {
  const t = useTheme();
  return (
    <div
      role="status"
      aria-live="polite"
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 12,
        padding: '14px 18px',
        marginBottom: 18,
        background: hexA(t.warn, 0.1),
        border: `1px solid ${hexA(t.warn, 0.35)}`,
        borderRadius: 10,
      }}
    >
      <span
        style={{
          flexShrink: 0,
          width: 8,
          height: 8,
          marginTop: 5,
          borderRadius: 999,
          background: t.warn,
        }}
      />
      <div>
        <div style={{ fontFamily: t.sans, fontWeight: 600, fontSize: 13.5, color: t.text }}>
          No Kubernetes cluster configured or cluster unreachable
        </div>
        <div style={{ fontSize: 12.5, color: t.textSoft, marginTop: 4, lineHeight: 1.55 }}>
          Set <code style={{ fontFamily: t.mono, fontSize: 12 }}>COOKER_KUBECONFIG</code> (or mount a
          kubeconfig at the default path) and restart Cooker. The backend returned HTTP 503.
        </div>
      </div>
    </div>
  );
}

export default function KubernetesPage() {
  const t = useTheme();
  const namespaces = useKubernetesStore((s) => s.namespaces);
  const workloads = useKubernetesStore((s) => s.workloads);
  const selectedNamespace = useKubernetesStore((s) => s.selectedNamespace);
  const loading = useKubernetesStore((s) => s.loading);
  const clusterUnavailable = useKubernetesStore((s) => s.clusterUnavailable);
  const fetchNamespaces = useKubernetesStore((s) => s.fetchNamespaces);
  const fetchWorkloads = useKubernetesStore((s) => s.fetchWorkloads);
  const setNamespace = useKubernetesStore((s) => s.setNamespace);

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
              disabled={clusterUnavailable}
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

      {!loading && clusterUnavailable && <ClusterUnavailableBanner />}

      <Card pad={0}>
        {/* Cluster unavailable (503) — show informational empty state, not a broken table. */}
        {!loading && clusterUnavailable ? (
          <EmptyState
            title="Cluster not reachable."
            body="Cooker could not connect to a Kubernetes cluster. Configure a kubeconfig and restart the server to see live workloads here."
          />
        ) : /* Empty cluster (200 + []) vs loading */
        !loading && workloads.length === 0 ? (
          <EmptyState
            title="No workloads found."
            body="Point Cooker at a kubeconfig to see Deployments, StatefulSets, and DaemonSets live."
            action={
              <div style={{ display: 'flex', gap: 10, justifyContent: 'center', flexWrap: 'wrap' }}>
                <Btn kind="primary" icon="layers">
                  Pick a kubeconfig
                </Btn>
                <a
                  href="https://github.com/santapong/Cooker/blob/main/docs/user-guide/operations/architecture.md"
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 6,
                    padding: '8px 16px',
                    border: '1px solid currentColor',
                    borderRadius: 7,
                    fontSize: 13.5,
                    color: 'inherit',
                    textDecoration: 'none',
                    opacity: 0.7,
                  }}
                >
                  Architecture guide ↗
                </a>
              </div>
            }
          />
        ) : (
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
        )}
      </Card>
    </div>
  );
}
