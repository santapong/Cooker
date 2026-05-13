import { useEffect, useState } from 'react';
import { registryApi } from '../api/registry';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import {
  Btn,
  Card,
  EmptyState,
  Input,
  KBD,
  PageHeader,
  Pill,
  SectionLabel,
} from '../components/ui/atoms';
import { Icon } from '../components/ui/Icon';

export default function RegistryPage() {
  const t = useTheme();
  const [repos, setRepos] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<string | null>(null);
  const [tags, setTags] = useState<string[]>([]);
  const [tagsLoading, setTagsLoading] = useState(false);

  useEffect(() => {
    registryApi
      .listRepositories()
      .then((r) => setRepos(r.repositories ?? []))
      .catch(() => setRepos([]))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (!selected) {
      setTags([]);
      return;
    }
    setTagsLoading(true);
    registryApi
      .listTags(selected)
      .then((r) => setTags(r.tags ?? []))
      .catch(() => setTags([]))
      .finally(() => setTagsLoading(false));
  }, [selected]);

  const filtered = search
    ? repos.filter((r) => r.toLowerCase().includes(search.toLowerCase()))
    : repos;

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="OCI distribution-spec v1.1"
        title="Registry"
        subtitle="Browse repositories Cooker can push to and pull from. Click a repo to inspect its tags and OCI referrers (signatures, SBOMs, build provenance)."
        actions={
          <>
            <Btn kind="ghost" icon="cog">
              Configure
            </Btn>
            <Btn kind="primary" icon="layers">
              Pull image
            </Btn>
          </>
        }
      />

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: selected ? '1fr 360px' : '1fr',
          gap: 22,
        }}
      >
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
              Repositories
            </span>
            <Pill>{repos.length}</Pill>
            <div style={{ flex: 1 }} />
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                background: t.bg,
                border: `1px solid ${t.line}`,
                borderRadius: 8,
                padding: '4px 8px',
                width: 280,
              }}
            >
              <Icon name="search" size={13} style={{ color: t.textMute }} />
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Filter repositories…"
                style={{
                  flex: 1,
                  background: 'transparent',
                  border: 'none',
                  outline: 'none',
                  color: t.text,
                  fontFamily: t.mono,
                  fontSize: 12,
                  padding: '4px 0',
                }}
              />
              <KBD>/</KBD>
            </div>
          </div>

          {loading ? (
            <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
          ) : filtered.length === 0 ? (
            // Empty-state two-CTA — W11 §Indie step 2 (PR #66).
            <EmptyState
              title={search ? 'No matches.' : 'No repositories yet.'}
              body={
                search ? (
                  <>Try a different filter, or clear it to see everything.</>
                ) : (
                  <>Push your first image — pipelines do this automatically once you wire one up.</>
                )
              }
              action={
                search ? undefined : (
                  <div style={{ display: 'flex', gap: 10, justifyContent: 'center', flexWrap: 'wrap' }}>
                    <Btn kind="primary" icon="layers">
                      Connect a registry
                    </Btn>
                    <a
                      href="https://github.com/santapong/Cooker/blob/main/docs/user-guide/guides/registries.md"
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
                      Registry guide ↗
                    </a>
                  </div>
                )
              }
            />
          ) : (
            <div>
              {filtered.map((r, i) => (
                <div
                  key={r}
                  onClick={() => setSelected(r)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 12,
                    padding: '12px 18px',
                    borderTop: `1px solid ${t.lineSoft}`,
                    background:
                      selected === r
                        ? hexA(t.accent, 0.06)
                        : i % 2 === 1
                          ? hexA(t.line, 0.18)
                          : 'transparent',
                    cursor: 'pointer',
                    borderLeft: `3px solid ${selected === r ? t.accent : 'transparent'}`,
                  }}
                >
                  <Icon name="layers" size={14} style={{ color: t.textMute }} />
                  <span style={{ fontFamily: t.mono, fontSize: 13, color: t.text, flex: 1 }}>
                    {r}
                  </span>
                  <Icon name="arrow" size={13} style={{ color: t.textMute }} />
                </div>
              ))}
            </div>
          )}
        </Card>

        {selected && (
          <Card pad={0}>
            <div
              style={{
                padding: '14px 18px',
                borderBottom: `1px solid ${t.line}`,
                display: 'flex',
                alignItems: 'center',
                gap: 10,
              }}
            >
              <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text, flex: 1 }}>
                {selected}
              </span>
              <button
                onClick={() => setSelected(null)}
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
            <div style={{ padding: 18, display: 'flex', flexDirection: 'column', gap: 14 }}>
              <SectionLabel>Tags</SectionLabel>
              {tagsLoading ? (
                <div style={{ color: t.textMute, fontSize: 13 }}>Loading tags…</div>
              ) : tags.length === 0 ? (
                <div
                  style={{
                    color: t.textMute,
                    fontSize: 13,
                    fontStyle: 'italic',
                    fontFamily: t.mono,
                  }}
                >
                  No tags published.
                </div>
              ) : (
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {tags.map((tag) => (
                    <Pill key={tag}>{tag}</Pill>
                  ))}
                </div>
              )}
              <Input value={selected} readOnly style={{ fontFamily: t.mono }} />
              <Btn kind="secondary" icon="arrow">
                Pull this repository
              </Btn>
            </div>
          </Card>
        )}
      </div>
    </div>
  );
}
