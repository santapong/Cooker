import { useState } from 'react';
import type { Environment } from '../../types/environment';
import EnvironmentKeys from './EnvironmentKeys';
import { Field, TextInput } from '../ui/form';
import './environment-editing.css';

export default function ServiceEnvironmentSelector({ services, environment, scope, bindings, onChange, disabled = false, allowNamedService = false, deploymentKind = 'compose' }: { services: readonly string[]; environment?: Environment; scope?: 'service' | ''; bindings: Record<string, string[]>; onChange: (scope: 'service', bindings: Record<string, string[]>) => void; disabled?: boolean; allowNamedService?: boolean; deploymentKind?: 'compose' | 'dockerfile' }) {
  const [named, setNamed] = useState('');
  const dockerfile = deploymentKind === 'dockerfile';
  const names = dockerfile ? ['deploy'] : [...new Set([...services, ...Object.keys(bindings)])].sort();
  return <section className="service-environment-selector" aria-label="Service environment bindings">
    <h3>{dockerfile ? 'Choose keys for the running app' : 'Choose keys for each service'}</h3>
    <p className="workspace-note">Configuration and protected key names can be selected together. Protected values stay hidden. Selecting an environment alone does not narrow existing legacy inheritance.</p>
    {scope !== 'service' ? <><p role="status">This app currently uses legacy environment inheritance. Choose explicit bindings to stop passing every linked environment value to every service.</p><button type="button" className="hud-btn" disabled={disabled} onClick={() => onChange('service', Object.fromEntries(names.map((name) => [name, []])))}>Choose explicit service bindings</button></> : <>
      <p>{dockerfile ? 'The deploy component receives only selected linked environment keys at runtime. An empty selection passes none. These selections do not supply Dockerfile build arguments or image-build secrets.' : "Explicit bindings are enabled. A service with no selected keys receives no linked environment values. Compose's own declared values are separate."}</p>
      {names.map((name) => <div key={name} className="environment-service"><h4>{name}</h4><EnvironmentKeys environment={environment} value={bindings[name] ?? []} onChange={(keys) => onChange('service', { ...bindings, [name]: keys })} disabled={disabled} label={`Keys for ${name}`} /></div>)}
      {!names.length && <p>Service names have not been reported. Review the Compose source to discover services before binding keys.</p>}
      {allowNamedService && !dockerfile && <div className="environment-key-add"><Field label="Existing Compose service name" hint="Use a service declared in the source. The server validates it before deployment."><TextInput value={named} maxLength={128} onChange={(event) => setNamed(event.target.value)} disabled={disabled} /></Field><button type="button" className="hud-btn" disabled={disabled || !named.trim() || names.length >= 128 || names.includes(named.trim())} onClick={() => { onChange('service', { ...bindings, [named.trim()]: [] }); setNamed(''); }}>Add service binding</button></div>}
    </>}
  </section>;
}
