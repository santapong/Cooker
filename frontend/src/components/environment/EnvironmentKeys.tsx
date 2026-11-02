import { useState } from 'react';
import type { Environment } from '../../types/environment';
import { environmentKeyPattern, knownEnvironmentKeys, ordinaryValues, toggleEnvironmentKey } from '../../utils/environmentEditing';
import { Check, Field, TextInput } from '../ui/form';
import './environment-editing.css';

/** Metadata only: selecting a protected key never requests its value. */
export default function EnvironmentKeys({ environment, value, onChange, disabled = false, label = 'Environment keys' }: { environment?: Environment; value: readonly string[]; onChange: (keys: string[]) => void; disabled?: boolean; label?: string }) {
  const [manual, setManual] = useState('');
  const [error, setError] = useState<string | null>(null);
  const known = knownEnvironmentKeys(environment);
  const keys = [...new Set([...known, ...value])].sort();
  return <fieldset className="environment-key-picker" disabled={disabled}>
    <legend>{label}</legend>
    {keys.map((key) => <div key={key} className="environment-key-row"><Check label={<><code>{key}</code> · {environment?.secretKeys?.includes(key) ? 'Protected · value hidden' : Object.prototype.hasOwnProperty.call(ordinaryValues(environment), key) ? <>Readable · <code>{ordinaryValues(environment)[key]}</code></> : 'Not reported by this environment'}</>} checked={value.includes(key)} disabled={!value.includes(key) && value.length >= 256} onChange={(selected) => onChange(toggleEnvironmentKey(value, key, selected))} /></div>)}
    {!keys.length && <p>No key names are reported. This does not prove that an external provider has no secrets.</p>}
    <div className="environment-key-add"><Field label="Additional key name" hint="Use an existing key name only. The server verifies availability at review and execution."><TextInput value={manual} maxLength={256} placeholder="DATABASE_PASSWORD" onChange={(event) => { setManual(event.target.value); setError(null); }} /></Field><button type="button" className="hud-btn" disabled={!manual.trim() || value.length >= 256} onClick={() => { const key = manual.trim(); if (!environmentKeyPattern.test(key)) { setError('Use a valid environment key name.'); return; } onChange(toggleEnvironmentKey(value, key, true)); setManual(''); }}>Bind key name</button></div>
    {error && <p role="alert">{error}</p>}
    <p className="workspace-note">Only selected keys are bound here. Unreported selections stay visible. An external provider's key inventory may be incomplete; no protected values are read.</p>
  </fieldset>;
}
