import { useTheme } from '../../theme/ThemeProvider';
import { Card, EmptyState, Pill, StatusDot } from '../ui/atoms';
import { DataTable } from '../ui/DataTable';
import type { ContainerInfo } from '../../types/docker';

interface ContainersPanelProps {
  containers: ContainerInfo[];
  loading: boolean;
}

/**
 * Containers tab of the Docker page — lists running/exited containers on the
 * build host, or an empty-state banner while the Docker host transport isn't
 * configured (COOKER_DOCKER_HOST / P9.4).
 */
export default function ContainersPanel({ containers, loading }: ContainersPanelProps) {
  const t = useTheme();
  return (
    <Card pad={0}>
      <div
        style={{
          padding: '14px 18px',
          borderBottom: `1px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 12,
        }}
      >
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          Containers
        </span>
        <Pill>{containers.length}</Pill>
        <div style={{ flex: 1 }} />
      </div>
      {!loading && containers.length === 0 ? (
        <EmptyState
          title="No containers available."
          body="The Docker host transport is not configured (COOKER_DOCKER_HOST / P9.4). Container data will appear here once the transport is wired up."
        />
      ) : (
        <DataTable
          rows={containers}
          rowKey={(c) => c.id}
          empty={loading ? 'Loading…' : 'No containers running.'}
          columns={[
            {
              key: 'name',
              header: 'Name',
              render: (c) => (
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <StatusDot tone={c.state === 'running' ? 'good' : 'neutral'} />
                  <span style={{ fontFamily: t.mono, fontSize: 12 }}>{c.name}</span>
                </div>
              ),
            },
            {
              key: 'image',
              header: 'Image',
              render: (c) => (
                <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                  {c.image}
                </span>
              ),
            },
            {
              key: 'state',
              header: 'State',
              width: '120px',
              render: (c) => (
                <Pill tone={c.state === 'running' ? 'good' : 'neutral'}>{c.state}</Pill>
              ),
            },
            {
              key: 'status',
              header: 'Status',
              width: '200px',
              render: (c) => (
                <span style={{ color: t.textSoft, fontSize: 12 }}>{c.status}</span>
              ),
            },
          ]}
        />
      )}
    </Card>
  );
}
