import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useAuth } from '../../auth/OIDCProvider';
import { useUIStore } from '../../stores/uiStore';
import { useConfigurationDraftStore } from '../../stores/configurationDraftStore';
import { usePipelineStore } from '../../stores/pipelineStore';
import InstrumentRail from './InstrumentRail';
import TopStrip from './TopStrip';
import { navItemVisible, NAV_ITEMS } from './navItems';
import './shell.css';
import '../../styles/workspace.css';

export default function AppShell({ children }: { children: ReactNode }) {
  const calm = useUIStore((s) => s.calmMode);
  const theme = useUIStore((s) => s.themeMode);
  const { user } = useAuth();
  const owner = user?.sub ?? null;
  useLayoutEffect(() => {
    useConfigurationDraftStore.getState().setOwner(owner);
    usePipelineStore.getState().clearDrafts();
    return () => {
      useConfigurationDraftStore.getState().setOwner(null);
      usePipelineStore.getState().clearDrafts();
    };
  }, [owner]);
  const { pathname } = useLocation();
  const drawer = useRef<HTMLDialogElement>(null);
  const search = useRef<HTMLDialogElement>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const [mobile, setMobile] = useState(() => window.matchMedia('(max-width: 860px)').matches);
  useEffect(() => {
    document.documentElement.dataset.calm = calm ? 'true' : 'false';
    document.documentElement.dataset.theme = theme;
    return () => { delete document.documentElement.dataset.theme; };
  }, [calm, theme]);
  useEffect(() => {
    const media = window.matchMedia('(max-width: 860px)');
    const update = () => setMobile(media.matches);
    media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, []);
  useEffect(() => { drawer.current?.close(); search.current?.close(); }, [pathname, mobile]);
  const openSearch = () => {
    drawer.current?.close();
    setQuery('');
    search.current?.showModal();
    searchInput.current?.focus();
  };
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && (search.current?.open || drawer.current?.open)) {
        event.preventDefault();
        search.current?.close();
        drawer.current?.close();
        return;
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        drawer.current?.close();
        setQuery('');
        search.current?.showModal();
        searchInput.current?.focus();
      }
    };
    document.addEventListener('keydown', keydown);
    return () => document.removeEventListener('keydown', keydown);
  }, []);
  const pages = NAV_ITEMS.filter((item) => navItemVisible(item, user?.roles) && item.label.toLowerCase().includes(query.toLowerCase()));
  return (
    <div className="shell">
      <a className="workspace-skip" href="#main">Skip to workspace</a>
      {!mobile && <InstrumentRail onSearch={openSearch} />}
      <TopStrip onOpenNavigation={() => drawer.current?.showModal()} onSearch={openSearch} />
      <main id="main" className="shell-main" tabIndex={-1}>{children}<footer className="workspace-footer">Cooker / Built to be understood.<Link to="/docs">Need a hand? Read the guides ↗</Link></footer></main>
      {mobile && <dialog ref={drawer} className="workspace-drawer" aria-label="Workspace navigation">
        <button className="drawer-close hud-btn" type="button" onClick={() => drawer.current?.close()} aria-label="Close workspace navigation">Close ×</button>
        <InstrumentRail onSearch={openSearch} onNavigate={() => drawer.current?.close()} />
      </dialog>}
      <dialog ref={search} className="workspace-search-dialog" aria-labelledby="workspace-search-title">
        <div className="workspace-dialog-head"><h2 id="workspace-search-title">Find your next step</h2><button className="hud-btn" type="button" onClick={() => search.current?.close()} aria-label="Close page search">Close ×</button></div>
        <label className="field">Search pages<input ref={searchInput} className="input" type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Apps, pipelines, Cloud…" /></label>
        <div className="workspace-search-results">{pages.map((item) => <Link key={item.id} to={item.to} onClick={() => search.current?.close()}>{item.label}<span aria-hidden="true">→</span></Link>)}</div>
        {!pages.length && <p role="status">No matching pages. Try another name.</p>}
        <p className="workspace-note">Search navigation pages · Escape to close</p>
      </dialog>
    </div>
  );
}
