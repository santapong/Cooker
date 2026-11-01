import { activeNavId, NAV_ITEMS, navItemById, type NavItem } from './navItems';

export interface Crumb {
  label: string;
  /** Link target; absent on the current (last) crumb. */
  to?: string;
  /** Render in the mono face — ids, run numbers. */
  mono?: boolean;
}

/** Path words that are section names rather than ids. */
const WORDS: Record<string, string> = {
  new: 'New app',
  edit: 'Edit',
  runs: 'Runs',
  deployments: 'Deployments',
};

function segmentsOf(path: string): string[] {
  return path.split('/').filter(Boolean);
}

/** Parent chain for nested nav items: Compose lives under Docker. */
function navChain(item: NavItem): NavItem[] {
  const chain: NavItem[] = [item];
  let current = item;
  for (;;) {
    const parent = NAV_ITEMS.find(
      (i) => i.id !== current.id && i.to !== '/' && current.to.startsWith(`${i.to}/`),
    );
    if (!parent) break;
    chain.unshift(parent);
    current = parent;
  }
  return chain;
}

/**
 * buildBreadcrumb — derive the top-strip breadcrumb from the pathname alone
 * (no data fetch at shell level). Section labels come from the rail table;
 * ids render in mono; known words (`runs`, `edit`, …) become labels.
 */
export function buildBreadcrumb(pathname: string): Crumb[] {
  const id = activeNavId(pathname);
  const item = id ? navItemById(id) : undefined;
  if (!item) {
    const segs = segmentsOf(pathname);
    if (segs.length === 0) return [{ label: 'Home' }];
    return segs.map((s, i) => ({
      label: s.charAt(0).toUpperCase() + s.slice(1),
      to: i < segs.length - 1 ? `/${segs.slice(0, i + 1).join('/')}` : undefined,
    }));
  }

  const crumbs: Crumb[] = navChain(item).map((n) => ({ label: n.label, to: n.to }));
  const base = segmentsOf(item.to);
  const rest = segmentsOf(pathname).slice(base.length);

  let acc = item.to;
  for (const seg of rest) {
    acc = `${acc}/${seg}`;
    const word = WORDS[seg];
    if (word) {
      crumbs.push({ label: word, to: acc });
    } else {
      // An id. Pipelines have no bare `/pipelines/:id` route — the editor is
      // the canonical page for a pipeline id.
      const to = item.id === 'pipelines' ? `${acc}/edit` : acc;
      crumbs.push({ label: seg, to, mono: true });
    }
  }

  // The current page is never a link.
  const last = crumbs[crumbs.length - 1];
  delete last.to;
  return crumbs;
}
