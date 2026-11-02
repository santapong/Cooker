import { Link } from 'react-router-dom';
import type { Stage, StageConfig } from '../../types/pipeline';
import EnvironmentKeys from '../environment/EnvironmentKeys';
import { useEnvironmentInventory } from '../environment/useEnvironmentInventory';
import { Field, Select } from '../ui/form';

export default function StageEnvironmentBindings({ stage, disabled, onEnvironment, onConfig }: { stage: Stage; disabled: boolean; onEnvironment: (environmentId: string) => void; onConfig: (config: Partial<StageConfig>) => void }) {
  const inventory = useEnvironmentInventory(stage.environmentId ?? '');
  const supported = stage.type === 'test' || stage.type === 'custom' || stage.type === 'deploy' && stage.config.deployRuntime === 'docker';
  const references = stage.config.secretRefs ?? [];
  return <section aria-label="Step environment bindings"><h3>Environment for this step</h3>
    <Field label="Step environment"><Select disabled={disabled} value={stage.environmentId ?? ''} onChange={(event) => onEnvironment(event.target.value)} options={[{ value: '', label: 'No linked environment' }, ...(stage.environmentId && !inventory.environments.some((environment) => environment.id === stage.environmentId) ? [{ value: stage.environmentId, label: `${stage.environmentId} · not in loaded inventory` }] : []), ...inventory.environments.map((environment) => ({ value: environment.id, label: environment.name }))]} /></Field>
    {inventory.unavailable && <p role="alert">Environment inventory is unavailable. Existing selections stay visible.</p>}{inventory.keysUnavailable && <p role="alert">Protected key inventory is unavailable. Unreported names may exist.</p>}{inventory.more && <button type="button" className="hud-btn" onClick={inventory.loadMore}>Load more environments</button>}
    {supported ? <><EnvironmentKeys environment={inventory.selected} value={references} onChange={(secretRefs) => onConfig({ secretRefs })} disabled={disabled} label="Keys for this step" />{!stage.environmentId && references.length > 0 && <p role="alert">Select an environment before running these key references.</p>}{references.some((key) => Object.prototype.hasOwnProperty.call(stage.config.env ?? {}, key)) && <p role="alert">A selected key also has a literal override in this step. The server rejects this collision; remove that override in the existing definition.</p>}</> : <><p>Key binding supports Test, Custom and Docker container steps. Build, Push, Approval and other deployment transports do not accept these runtime key references.</p>{references.length > 0 && <p role="alert">Existing unsupported references are retained: {references.join(', ')}. Remove or move them before running this definition.</p>}</>}
    <p className="workspace-note">The accepted run pins this environment and selected keys. Saving the pipeline does not start a run. <Link to={`/environments${stage.environmentId ? `?environment=${encodeURIComponent(stage.environmentId)}` : ''}`}>Manage values ↗</Link></p>
  </section>;
}
