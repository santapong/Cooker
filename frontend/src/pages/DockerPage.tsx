import { useEffect } from 'react';
import { useDockerStore } from '../stores/dockerStore';
import { useTheme } from '../theme/ThemeProvider';
import { Btn, Card, PageHeader, Pill, StatusDot } from '../components/ui/atoms';
import { DataTable } from '../components/ui/DataTable';

export default function DockerPage() {
  const t = useTheme();
  const { images, containers, loading, fetchImages, fetchContainers } = useDockerStore();

  useEffect(() => {
    fetchImages();
    fetchContainers();
  }, [fetchImages, fetchContainers]);

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="local docker host"
        title="Images & containers"
        subtitle="Browse images cached on the build host and inspect running containers. Use this to debug a build that won't push or a container that won't start."
        actions={
          <>
            <Btn kind="secondary" icon="layers">Pull image</Btn>
            <Btn kind="primary" icon="plus">Build image</Btn>
          </>
        }
      />

      <Card pad={0} style={{ marginBottom: 22 }}>
        <SectionHeader title="Images" pillCount={images.length} />
        <DataTable
          rows={images}
          rowKey={(img) => img.id}
          empty={loading ? 'Loading…' : 'No images cached locally yet.'}
          columns={[
            {
              key: 'repo',
              header: 'Repository',
              render: (img) => (
                <span style={{ fontFamily: t.mono, fontSize: 12 }}>
                  {img.repoTags?.[0]?.split(':')[0] || img.id.slice(0, 12)}
                </span>
              ),
            },
            {
              key: 'tag',
              header: 'Tag',
              width: '160px',
              render: (img) => (
                <Pill tone="neutral">{img.repoTags?.[0]?.split(':')[1] || 'latest'}</Pill>
              ),
            },
            {
              key: 'size',
              header: 'Size',
              width: '120px',
              align: 'right',
              render: (img) => (
                <span style={{ fontFamily: t.mono, fontSize: 12 }}>
                  {(img.size / 1024 / 1024).toFixed(1)} MB
                </span>
              ),
            },
            {
              key: 'created',
              header: 'Created',
              width: '160px',
              render: (img) => (
                <span style={{ color: t.textMute, fontSize: 12 }}>
                  {new Date(img.created).toLocaleDateString()}
                </span>
              ),
            },
          ]}
        />
      </Card>

      <Card pad={0}>
        <SectionHeader title="Containers" pillCount={containers.length} />
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
      </Card>
    </div>
  );
}

function SectionHeader({ title, pillCount }: { title: string; pillCount: number }) {
  const t = useTheme();
  return (
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
        {title}
      </span>
      <Pill>{pillCount}</Pill>
    </div>
  );
}
