import { useState, useEffect, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppModel } from '../types/app';
import { useTheme } from '../theme/ThemeProvider';
import { useUIStore } from '../stores/uiStore';
import {
  Btn,
  Card,
  EmptyState,
  PageHeader,
  Pill,
  StatusDot,
  type Tone,
} from '../components/ui/atoms';
import { SkeletonStack } from '../components/Skeleton';
import AppsGrid from '../components/apps/AppsGrid';
import type { RowApp } from '../components/apps/AppCard';

export default function AppsPage() {
  const t = useTheme();
  const mode = useUIStore((s) => s.mode);
  const navigate = useNavigate();
  const [apps, setApps] = useState<AppModel[]>([]);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState<'all' | 'yours' | 'broken'>('all');

  useEffect(() => {
    appsApi
      .list()
      .then(setApps)
      .catch(() => setApps([]))
      .finally(() => setLoading(false));
  }, []);

  const rows = useMemo<RowApp[]>(
    () =>
      apps.map((a) => {
        const env = a.deployTarget?.kind === 'kubernetes' ? a.deployTarget.namespace || 'prod' : 'dev';
        return {
          app: a,
          status: 'good' as Tone,
          env,
          envTone: env === 'prod' ? 'accent' : env === 'staging' || env === 'stg' ? 'cool' : 'neutral',
          image: a.registryRef || 'untagged',
          health: 99,
          runs: 0,
          lastDeploy: relativeTime(a.updatedAt),
          owner: ownerCode(a.name),
          team: teamGuess(a.githubRepo),
        };
      }),
    [apps],
  );

  const filtered = rows.filter((r) => {
    if (tab === 'broken') return r.status === 'bad' || r.status === 'warn';
    return true;
  });

  const stats = useMemo(
    () => [
      { label: 'Apps', value: String(apps.length), sub: apps.length === 0 ? 'connect a repo' : 'all healthy', tone: 'neutral' as Tone },
      { label: 'Runs / 24h', value: '—', sub: 'wire up to runs API', tone: 'neutral' as Tone },
      { label: 'Mean Build', value: '—', sub: 'no data yet', tone: 'neutral' as Tone },
      { label: 'Open incidents', value: '0', sub: 'all clear', tone: 'good' as Tone },
    ],
    [apps.length],
  );

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow={today()}
        title={
          <>
            {greeting()}.
            <br />
            <span style={{ color: t.textMute }}>{apps.length} apps cooking</span>
            {apps.length > 0 && (
              <>
                ,{' '}
                <span style={{ color: t.accent }}>all healthy.</span>
              </>
            )}
          </>
        }
        actions={
          mode === 'simple' ? (
            <>
              <Btn kind="secondary" icon="play" onClick={() => navigate('/apps/new')}>
                Deploy a service
              </Btn>
              <Btn kind="ink" icon="spark" onClick={() => navigate('/apps/new')}>
                Try a template
              </Btn>
            </>
          ) : (
            <>
              <Btn kind="ghost" icon="cog">Filters</Btn>
              <Btn kind="secondary" icon="layers">Import from Helm</Btn>
              <Btn kind="primary" icon="plus" onClick={() => navigate('/apps/new')}>
                New app
              </Btn>
            </>
          )
        }
      />

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(4, 1fr)',
          gap: 14,
          marginBottom: 26,
        }}
      >
        {stats.map((s) => (
          <Card
            key={s.label}
            pad={18}
            style={{
              borderLeft: `3px solid ${
                s.tone === 'good' ? t.good : s.tone === 'bad' ? t.bad : t.line
              }`,
            }}
          >
            <div
              style={{
                fontFamily: t.mono,
                fontSize: 10.5,
                letterSpacing: 1.2,
                textTransform: 'uppercase',
                color: t.textMute,
              }}
            >
              {s.label}
            </div>
            <div
              style={{
                fontFamily: t.serif,
                fontSize: 32,
                fontWeight: 500,
                color: t.text,
                marginTop: 6,
                letterSpacing: -0.5,
              }}
            >
              {s.value}
            </div>
            <div
              style={{
                fontSize: 12,
                color: s.tone === 'bad' ? t.bad : s.tone === 'good' ? t.good : t.textMute,
                marginTop: 4,
              }}
            >
              {s.sub}
            </div>
          </Card>
        ))}
      </div>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: mode === 'pro' ? '1fr 320px' : '1fr',
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
            <span
              style={{
                fontFamily: t.serif,
                fontSize: 18,
                fontWeight: 500,
                color: t.text,
              }}
            >
              Your services
            </span>
            <Pill>{rows.length}</Pill>
            <div style={{ flex: 1 }} />
            <div
              style={{
                display: 'flex',
                padding: 2,
                background: t.surfaceAlt,
                border: `1px solid ${t.line}`,
                borderRadius: 6,
                fontSize: 11.5,
              }}
            >
              {(['all', 'yours', 'broken'] as const).map((label) => (
                <span
                  key={label}
                  onClick={() => setTab(label)}
                  style={{
                    padding: '4px 10px',
                    borderRadius: 4,
                    background: tab === label ? t.surface : 'transparent',
                    color: tab === label ? t.text : t.textMute,
                    fontFamily: t.mono,
                    textTransform: 'uppercase',
                    letterSpacing: 0.6,
                    cursor: 'pointer',
                    fontWeight: 600,
                  }}
                >
                  {label}
                </span>
              ))}
            </div>
          </div>

          {loading ? (
            <div style={{ padding: 18 }}>
              <SkeletonStack rows={4} />
            </div>
          ) : filtered.length === 0 ? (
            // Empty-state CTA — Indie step 2 (W11-A3, PR #50).
            // Replaces local EmptyServices component with the shared EmptyState atom.
            <EmptyState
              title="Nothing cooking yet."
              body="Connect Cooker to a GitHub repo and we'll handle build → ship → run end to end."
              action={
                <div style={{ display: 'flex', gap: 10, justifyContent: 'center', flexWrap: 'wrap' }}>
                  <Btn kind="primary" icon="plus" onClick={() => navigate('/apps/new')}>
                    Create your first App
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
            <AppsGrid rows={filtered} mode={mode} onSelect={(id) => navigate(`/apps/${id}`)} />
          )}
        </Card>

        {mode === 'pro' && (
          <Card pad={0}>
            <div style={{ padding: '14px 18px', borderBottom: `1px solid ${t.line}` }}>
              <span
                style={{
                  fontFamily: t.serif,
                  fontSize: 18,
                  fontWeight: 500,
                  color: t.text,
                }}
              >
                Activity
              </span>
            </div>
            <ActivityFeed apps={apps} />
          </Card>
        )}
      </div>
    </div>
  );
}

function ActivityFeed({ apps }: { apps: AppModel[] }) {
  const t = useTheme();
  if (apps.length === 0) {
    return (
      <div style={{ padding: '24px 18px', color: t.textMute, fontSize: 13, textAlign: 'center' }}>
        No activity yet — create an app to start cooking.
      </div>
    );
  }
  const events = apps.slice(0, 6).map((a) => ({
    t: relativeTime(a.updatedAt),
    who: a.name,
    what: 'updated',
    tone: 'cool' as Tone,
  }));
  return (
    <div style={{ padding: '10px 6px 14px' }}>
      {events.map((e, i) => (
        <div
          key={i}
          style={{
            display: 'grid',
            gridTemplateColumns: '60px 16px 1fr',
            alignItems: 'flex-start',
            gap: 8,
            padding: '8px 14px',
          }}
        >
          <span
            style={{
              fontFamily: t.mono,
              fontSize: 10.5,
              color: t.textMute,
              paddingTop: 3,
            }}
          >
            {e.t}
          </span>
          <div style={{ display: 'flex', justifyContent: 'center', paddingTop: 7, position: 'relative' }}>
            <StatusDot tone={e.tone} />
            {i < events.length - 1 && (
              <span
                style={{
                  position: 'absolute',
                  top: 16,
                  bottom: -8,
                  width: 1,
                  background: t.line,
                }}
              />
            )}
          </div>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontFamily: t.mono, fontSize: 12, color: t.text, fontWeight: 600 }}>
              {e.who}
            </div>
            <div style={{ fontSize: 12, color: t.textSoft, marginTop: 1 }}>{e.what}</div>
          </div>
        </div>
      ))}
    </div>
  );
}

function today(): string {
  return new Date().toLocaleDateString(undefined, {
    weekday: 'long',
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  });
}

function greeting(): string {
  const h = new Date().getHours();
  if (h < 12) return 'Good morning';
  if (h < 17) return 'Good afternoon';
  return 'Good evening';
}

function relativeTime(iso?: string): string {
  if (!iso) return '—';
  const ms = Date.now() - new Date(iso).getTime();
  if (ms < 60_000) return 'just now';
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m ago`;
  if (ms < 86_400_000) return `${Math.floor(ms / 3_600_000)}h ago`;
  if (ms < 7 * 86_400_000) return `${Math.floor(ms / 86_400_000)}d ago`;
  return new Date(iso).toLocaleDateString();
}

function ownerCode(name: string): string {
  return name.slice(0, 2).toUpperCase();
}

function teamGuess(repo: string): string {
  const owner = repo.split('/')[0] || '—';
  return owner.toLowerCase();
}
