import { useState, useEffect, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppModel } from '../types/app';
import { useTheme } from '../theme/ThemeProvider';
import { useUIStore } from '../stores/uiStore';
import { hexA } from '../theme/tokens';
import {
  Btn,
  Card,
  HealthBar,
  PageHeader,
  Pill,
  StatusDot,
  type Tone,
} from '../components/ui/atoms';
import { Icon } from '../components/ui/Icon';
import { SkeletonStack } from '../components/Skeleton';

interface RowApp {
  app: AppModel;
  status: Tone;
  env: string;
  envTone: Tone;
  image: string;
  health: number;
  runs: number;
  lastDeploy: string;
  owner: string;
  team: string;
}

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
            <EmptyServices onCreate={() => navigate('/apps/new')} />
          ) : (
            <div>
              <div style={rowGrid(mode, true)}>
                {colHeads(mode).map((h) => (
                  <div
                    key={h}
                    style={{
                      fontFamily: t.mono,
                      fontSize: 10.5,
                      letterSpacing: 1,
                      textTransform: 'uppercase',
                      color: t.textMute,
                      padding: '10px 14px',
                    }}
                  >
                    {h}
                  </div>
                ))}
              </div>
              {filtered.map((r, i) => (
                <div
                  key={r.app.id}
                  onClick={() => navigate(`/apps/${r.app.id}`)}
                  style={{
                    ...rowGrid(mode, false),
                    borderTop: `1px solid ${t.lineSoft}`,
                    background: i % 2 === 1 ? hexA(t.line, 0.18) : 'transparent',
                    cursor: 'pointer',
                  }}
                >
                  <div style={{ padding: '12px 14px', display: 'flex', alignItems: 'center', gap: 10 }}>
                    <StatusDot tone={r.status} pulse={r.status === 'ember'} />
                    <div>
                      <div
                        style={{
                          fontFamily: t.mono,
                          fontSize: 13,
                          color: t.text,
                          fontWeight: 600,
                        }}
                      >
                        {r.app.name}
                      </div>
                      <div style={{ fontSize: 11, color: t.textMute, marginTop: 2 }}>
                        {r.team}
                      </div>
                    </div>
                  </div>

                  <div style={{ padding: '12px 14px', display: 'flex', alignItems: 'center', gap: 8 }}>
                    <Pill tone={r.envTone}>{r.env}</Pill>
                    <span
                      style={{
                        fontFamily: t.mono,
                        fontSize: 11.5,
                        color: t.textSoft,
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                      }}
                    >
                      {r.image}
                    </span>
                  </div>

                  <div style={{ padding: '12px 14px', display: 'flex', alignItems: 'center', gap: 10 }}>
                    <HealthBar value={r.health} />
                    <span
                      style={{
                        fontFamily: t.mono,
                        fontSize: 11,
                        color: t.textSoft,
                        minWidth: 40,
                      }}
                    >
                      {r.health}%
                    </span>
                  </div>

                  {mode === 'pro' && (
                    <div
                      style={{
                        padding: '12px 14px',
                        fontFamily: t.mono,
                        fontSize: 11.5,
                        color: t.textSoft,
                      }}
                    >
                      {r.runs}
                    </div>
                  )}

                  <div
                    style={{
                      padding: '12px 14px',
                      fontSize: 12,
                      color: r.status === 'ember' ? t.ember : t.textSoft,
                      display: 'flex',
                      alignItems: 'center',
                      gap: 8,
                    }}
                  >
                    {r.lastDeploy}
                  </div>

                  <div
                    style={{
                      padding: '12px 14px',
                      display: 'flex',
                      alignItems: 'center',
                      gap: 6,
                      justifyContent: 'flex-end',
                    }}
                  >
                    <span
                      style={{
                        width: 22,
                        height: 22,
                        borderRadius: 999,
                        background: t.accentDeep,
                        color: '#FFF8EE',
                        display: 'grid',
                        placeItems: 'center',
                        fontFamily: t.mono,
                        fontWeight: 700,
                        fontSize: 9.5,
                      }}
                    >
                      {r.owner}
                    </span>
                    <Icon name="arrow" size={13} style={{ color: t.textMute }} />
                  </div>
                </div>
              ))}
            </div>
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

function EmptyServices({ onCreate }: { onCreate: () => void }) {
  const t = useTheme();
  return (
    <div style={{ padding: '40px 28px', textAlign: 'center' }}>
      <div
        style={{
          fontFamily: t.serif,
          fontSize: 22,
          fontWeight: 500,
          color: t.text,
          letterSpacing: -0.3,
        }}
      >
        Nothing in the kitchen yet.
      </div>
      <div style={{ color: t.textSoft, marginTop: 10, fontSize: 13.5, lineHeight: 1.6 }}>
        Point Cooker at a GitHub repo and we'll handle build → ship → run end to end.
      </div>
      <div style={{ marginTop: 22 }}>
        <Btn kind="primary" icon="plus" onClick={onCreate}>
          Connect a repo
        </Btn>
      </div>
    </div>
  );
}

function rowGrid(mode: 'simple' | 'pro', header: boolean): React.CSSProperties {
  return {
    display: 'grid',
    gridTemplateColumns:
      mode === 'pro'
        ? 'minmax(180px,1.4fr) 200px 200px 80px 130px 90px'
        : 'minmax(180px,1.4fr) 200px 200px 130px 90px',
    alignItems: 'center',
    minHeight: header ? 0 : 50,
  };
}

function colHeads(mode: 'simple' | 'pro'): string[] {
  return mode === 'pro'
    ? ['Service', 'Environment / image', 'Health', 'Runs', 'Last deploy', '']
    : ['Service', 'Environment / image', 'Health', 'Last deploy', ''];
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
