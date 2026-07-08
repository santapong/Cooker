import { useTheme } from '../../theme/ThemeProvider';
import { Card, EmptyState, Pill } from '../ui/atoms';
import { DataTable } from '../ui/DataTable';
import type { ImageInfo } from '../../types/docker';

interface ImagesPanelProps {
  images: ImageInfo[];
  loading: boolean;
}

/**
 * Images tab of the Docker page — lists images cached on the build host, or
 * an empty-state banner while the Docker host transport isn't configured
 * (COOKER_DOCKER_HOST / P9.4).
 */
export default function ImagesPanel({ images, loading }: ImagesPanelProps) {
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
          Images
        </span>
        <Pill>{images.length}</Pill>
        <div style={{ flex: 1 }} />
      </div>
      {/* Empty-state — W11 §Indie step 2 (PR #66). Transport not configured → [] from backend. */}
      {!loading && images.length === 0 ? (
        <EmptyState
          title="No images available."
          body="The Docker host transport is not configured (COOKER_DOCKER_HOST / P9.4). Images will appear here once the transport is wired up."
          action={
            <a
              href="https://github.com/santapong/Cooker/blob/main/docs/user-guide/operations/docker-builds.md"
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
              Docker builds guide ↗
            </a>
          }
        />
      ) : (
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
      )}
    </Card>
  );
}
