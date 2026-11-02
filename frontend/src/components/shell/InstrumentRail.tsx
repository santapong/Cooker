import { Link, useLocation } from 'react-router-dom';
import { useAuth } from '../../auth/OIDCProvider';
import { activeNavId, navItemVisible, NAV_GROUPS, NAV_ITEMS } from './navItems';
import CookerMark from '../brand/CookerMark';

export default function InstrumentRail({ onNavigate, onSearch }: { onNavigate?: () => void; onSearch: () => void }) {
  const { pathname } = useLocation();
  const { user } = useAuth();
  const active = activeNavId(pathname);
  return (
    <nav className="rail" aria-label="Primary">
      <Link to="/" className="rail-logo" aria-label="Cooker — home" onClick={onNavigate}>
        <CookerMark />
        <span aria-hidden="true">[</span> Cooker<span className="brand-period">.</span> <span aria-hidden="true">]</span>
      </Link>
      <p className="rail-intro">Your work, in view.</p>
      <button className="rail-search" onClick={onSearch} type="button">Find a page <kbd>⌘ K</kbd></button>
      {NAV_GROUPS.map((group) => (
        <div key={group.id} className="rail-group" role="group" aria-label={group.label}>
          <p className="rail-group-label">{group.label}</p>
          {NAV_ITEMS.filter((item) => item.group === group.id && navItemVisible(item, user?.roles)).map((item) => {
            const Icon = item.icon;
            return <Link key={item.id} to={item.to} onClick={onNavigate}
              className={active === item.id ? 'rail-item is-active' : 'rail-item'}
              aria-current={active === item.id ? 'page' : undefined}>
              <Icon /><span>{item.label}</span>
            </Link>;
          })}
        </div>
      ))}
      <div className="rail-foot"><strong>Cooker workspace</strong><span>{user?.roles[0] ?? 'Connected workspace'}</span></div>
    </nav>
  );
}
