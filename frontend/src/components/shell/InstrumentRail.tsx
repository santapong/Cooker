import { Link, useLocation } from 'react-router-dom';
import { activeNavId, NAV_GROUPS, NAV_ITEMS } from './navItems';

/** 64px icon-only left rail — grouped with hairline gaps, lit ember dot on the active item. */
export default function InstrumentRail() {
  const { pathname } = useLocation();
  const active = activeNavId(pathname);

  return (
    <nav className="rail" aria-label="Primary">
      <Link to="/" className="rail-logo" title="cooker" aria-label="cooker — home">
        <span className="rail-logo-core" />
      </Link>

      {NAV_GROUPS.map((group, gi) => (
        <div key={group.id} className="rail-group" role="group" aria-label={group.label}>
          {gi > 0 && <span className="rail-gap" aria-hidden="true" />}
          {NAV_ITEMS.filter((item) => item.group === group.id).map((item) => {
            const Icon = item.icon;
            const isActive = active === item.id;
            return (
              <Link
                key={item.id}
                to={item.to}
                className={isActive ? 'rail-item is-active' : 'rail-item'}
                aria-current={isActive ? 'page' : undefined}
                aria-label={item.label}
                title={item.label}
              >
                <Icon />
              </Link>
            );
          })}
        </div>
      ))}
    </nav>
  );
}
