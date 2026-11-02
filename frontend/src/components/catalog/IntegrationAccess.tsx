import type { IntegrationInstallation, IntegrationInventory } from '../../types/integrations';
import { timeAgo } from '../../utils/time';
import Badge from '../ui/Badge';
import './catalog.css';

interface Props {
  inventory?: IntegrationInventory | null;
  compact?: boolean;
  scope?: 'project' | 'connection';
  projects?: readonly { id: string; name: string }[];
}

const installationTypes = new Map<string, string>([
  ['marketplace', 'Marketplace installation'],
  ['external', 'External installation'],
]);

const installationStates = new Map<string, string>([
  ['error', 'Error'], ['onboarding', 'Onboarding'], ['pending', 'Pending'], ['ready', 'Ready'],
  ['resumed', 'Resumed'], ['suspended', 'Suspended'], ['uninstalled', 'Uninstalled'],
]);

const inventoryErrors = new Map<string, string>([
  ['unauthorized', 'Integration inventory is unavailable with this connection’s authentication.'],
  ['forbidden', 'Access denied for integration inventory. Installation count is unknown.'],
  ['unsupported', 'Integration inventory is not supported by this connection.'],
  ['rate_limited', 'Integration inventory could not be refreshed because reads are rate limited.'],
]);

function validTime(value?: string): value is string {
  return !!value && Number.isFinite(Date.parse(value));
}

function ObservationTime({ label, value }: { label: string; value?: string }) {
  return validTime(value) ? <p className="workspace-note">{label}: <time dateTime={value} title={value}>{timeAgo(value)}</time></p> : null;
}

function InstallationRecord({ installation, compact, projects }: { installation: IntegrationInstallation; compact: boolean; projects: Props['projects'] }) {
  const projectIds = [...new Set(installation.projectIds ?? [])];
  const name = installation.name || installation.slug || installation.integrationId || 'Unnamed integration';
  const access = installation.projectAccess === 'all_projects' ? 'All projects (provider permission)'
    : installation.projectAccess === 'selected_projects' ? 'Selected projects (provider permission)' : 'Unknown project access';
  return <article className="catalog-deployment" role="listitem">
    {compact ? <h5>{name}</h5> : <h3>{name}</h3>}
    <p>{installationTypes.get(installation.installationType) || 'Unknown installation type'}</p>
    <p>Provider-reported installation state: {installationStates.get(installation.providerStatus) || 'Unknown'}</p>
    <p><strong>Allowed project access:</strong> {access}</p>
    {projectIds.length > 0 ? <div className="workspace-note">Project IDs visible in this selected scope:
      <ul>{projectIds.map((id) => {
        const project = projects?.find((candidate) => candidate.id === id);
        return <li key={id}>{project?.name ? <>{project.name} · </> : null}<code>{id}</code></li>;
      })}</ul>
    </div> : <p className="workspace-note">No project IDs listed in this selected scope.</p>}
    <p>Installation <code>{installation.id}</code> · Integration <code>{installation.integrationId}</code></p>
  </article>;
}

/** Uses only the existing catalog/connection observation; rendering never requests provider data. */
export default function IntegrationAccess({ inventory, compact = false, scope = 'project', projects }: Props) {
  const observed = !!inventory && inventory.complete && validTime(inventory.observedAt) && Array.isArray(inventory.items);
  const current = observed && inventory.state === 'available';
  const state = observed ? current ? 'Available' : inventory.state === 'stale' ? 'Stale' : 'Observation state unknown'
    : inventory?.state === 'unavailable' ? 'Unavailable' : 'Not observed';
  const items = observed ? inventory.items : [];
  const selectedScope = scope === 'project' ? 'this project' : 'this connection’s selected projects';
  const error = inventory?.error || inventoryErrors.get(inventory?.errorCode ?? '');
  const records = <div role="list" aria-label="Integration installation records">
    {items.map((installation) => <InstallationRecord key={installation.id} installation={installation} compact={compact} projects={projects} />)}
  </div>;

  return <section className={compact ? 'catalog-flow-panel' : 'panel catalog-flow-panel'} aria-label="Installed integration access">
    <div className="workspace-section-head">
      {compact ? <h4>Integration access</h4> : <h2>Installed integrations</h2>}
      <Badge variant="muted">{state}</Badge>
    </div>
    <p className="workspace-note">{compact
      ? 'Read-only installation metadata. Resource bindings, health, and SQL connectivity are not checked.'
      : 'Allowed project access describes an installation’s permission to access projects. It does not identify resource or database bindings. Installation state does not measure health; a reported Neon installation does not verify SQL connectivity.'}</p>
    {observed ? <>
      <ObservationTime label="Integration inventory observed" value={inventory.observedAt} />
      {!current && <p className="workspace-note">Showing the last successful observation. These installation records have not been freshly confirmed.</p>}
      <p className="workspace-note">Complete installation observation for the selected scope. This inventory does not cover every Vercel product or provider resource.</p>
    </> : <p className="workspace-note">{state === 'Unavailable'
      ? 'Integration inventory is unavailable. Installation count is unknown.'
      : 'Integration inventory has not been observed for this scope. Installation count is unknown.'}</p>}
    <ObservationTime label="Integration inventory last attempted" value={inventory?.attemptedAt} />
    {error && <p className="workspace-error" role="status">{error}</p>}
    {observed && (items.length > 0 ? compact
      ? <details><summary>View {items.length} installation {items.length === 1 ? 'record' : 'records'} for this connection</summary>{records}</details>
      : records
      : <p>{current ? 'Vercel reported no installations with allowed access to ' : 'The last successful observation reported no installations with allowed access to '}{selectedScope}.</p>)}
  </section>;
}
