import { useEffect, useMemo } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useAuth } from '../../auth/OIDCProvider';
import { useCapabilitiesStore } from '../../stores/capabilitiesStore';
import { useUIStore } from '../../stores/uiStore';
import Badge from '../ui/Badge';
import { CalmIcon, LogoutIcon } from '../icons';
import { buildBreadcrumb } from './breadcrumb';

const CAPABILITY_LABELS: { key: 'cloudInventory' | 'aiTriage' | 'feedback'; label: string }[] = [
  { key: 'cloudInventory', label: 'Cloud inventory' },
  { key: 'aiTriage', label: 'AI triage' },
  { key: 'feedback', label: 'Feedback' },
];

/** 48px top strip — breadcrumb, capability badges, Calm toggle, user. */
export default function TopStrip() {
  const { pathname } = useLocation();
  const crumbs = useMemo(() => buildBreadcrumb(pathname), [pathname]);
  const { user, logout } = useAuth();

  const capabilities = useCapabilitiesStore((s) => s.capabilities);
  const fetchCapabilities = useCapabilitiesStore((s) => s.fetch);
  useEffect(() => {
    void fetchCapabilities();
  }, [fetchCapabilities]);

  const calm = useUIStore((s) => s.calmMode);
  const toggleCalm = useUIStore((s) => s.toggleCalmMode);

  const enabled = capabilities
    ? CAPABILITY_LABELS.filter((c) => capabilities[c.key])
    : [];

  return (
    <header className="strip">
      <nav className="strip-crumbs" aria-label="Breadcrumb">
        <ol>
          {crumbs.map((crumb, i) => {
            const isLast = i === crumbs.length - 1;
            const cls = crumb.mono ? 'mono' : undefined;
            return (
              <li key={`${i}-${crumb.label}`}>
                {i > 0 && (
                  <span className="strip-sep" aria-hidden="true">
                    /
                  </span>
                )}
                {crumb.to && !isLast ? (
                  <Link to={crumb.to} className={cls} title={crumb.mono ? crumb.label : undefined}>
                    {crumb.label}
                  </Link>
                ) : (
                  <span className={cls} aria-current={isLast ? 'page' : undefined} title={crumb.mono ? crumb.label : undefined}>
                    {crumb.label}
                  </span>
                )}
              </li>
            );
          })}
        </ol>
      </nav>

      <span className="strip-spacer" />

      {enabled.length > 0 && (
        <div className="strip-caps" aria-label="Enabled capabilities">
          {enabled.map((c) => (
            <Badge key={c.key} variant="muted">
              {c.label}
            </Badge>
          ))}
        </div>
      )}

      <button
        type="button"
        className={calm ? 'strip-toggle is-on' : 'strip-toggle'}
        aria-pressed={calm}
        onClick={toggleCalm}
        title="Calm mode — pause ambient and looping motion"
      >
        <CalmIcon width={14} height={14} />
        <span>Calm</span>
      </button>

      {user && (
        <div className="strip-user">
          <span className="strip-user-name" title={user.email}>
            {user.name || user.email}
          </span>
          {user.roles[0] && <Badge variant="muted">{user.roles[0]}</Badge>}
          <button type="button" className="strip-iconbtn" onClick={logout} title="Sign out" aria-label="Sign out">
            <LogoutIcon />
          </button>
        </div>
      )}
    </header>
  );
}
