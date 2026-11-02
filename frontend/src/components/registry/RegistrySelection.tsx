import { useEffect, useState } from 'react';
import { settingsApi } from '../../api/settings';
import type { RegistryConfig } from '../../types/infra';
import { registrySelectionState, type RegistrySubject } from '../../utils/registryAuthority';
import { Check } from '../ui/form';
import './registry-authority.css';

export interface RegistrySelectionProps {
  subject: RegistrySubject;
  value: readonly string[];
  onChange: (ids: string[]) => void;
  disabled?: boolean;
  label?: string;
  registries?: readonly RegistryConfig[];
  loading?: boolean;
  error?: string | null;
  onRetry?: () => void;
}

/** Metadata-only selection. The server rechecks grants and pins at admission. */
export default function RegistrySelection({ subject, value, onChange, disabled = false, label = 'Registry access', registries, loading, error, onRetry }: RegistrySelectionProps) {
  const [loaded, setLoaded] = useState<RegistryConfig[]>([]);
  const [pending, setPending] = useState(registries === undefined);
  const [failed, setFailed] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (registries !== undefined) return;
    let active = true;
    setPending(true);
    setFailed(false);
    void settingsApi.listRegistries().then((rows) => {
      if (!Array.isArray(rows) || rows.some((row) => !row || typeof row.id !== 'string' || typeof row.name !== 'string' || typeof row.url !== 'string')) throw new Error('Registry metadata unavailable.');
      if (active) setLoaded(rows);
    }).catch(() => {
      if (active) { setFailed(true); setLoaded([]); }
    }).finally(() => { if (active) setPending(false); });
    return () => { active = false; };
  }, [registries, revision]);
  const inventory = registries ?? loaded;
  const reading = registries === undefined ? pending : !!loading;
  const unreadable = registries === undefined ? failed : !!error;
  const state = registrySelectionState(reading || unreadable ? [] : inventory, subject, value);
  const selectable = !disabled && !reading && !unreadable && !!subject.id;
  return (
    <fieldset className="registry-selection" aria-busy={reading}>
      <legend>{label}</legend>
      <p className="registry-help">Select connections an administrator granted to this {subject.kind}. Credentials stay in Cooker; the server checks access again before work is accepted.</p>
      {!subject.id && <p role="status">Save this {subject.kind} first so an administrator can grant access to its ID. An unsaved {subject.kind} uses anonymous registry access.</p>}
      {reading && <p role="status">Loading registry access…</p>}
      {unreadable && <div role="alert"><p>Registry access could not be checked. Existing selections are retained.</p><button type="button" className="hud-btn" onClick={() => onRetry ? onRetry() : setRevision((v) => v + 1)}>Retry registry access</button></div>}
      {value.length === 0 && <p className="registry-help">Anonymous access — no saved registry credentials will be used. Private images may require an explicit grant and selection.</p>}
      {selectable && state.available.length === 0 && <p>No connections are granted to this {subject.kind}. Ask an administrator to grant access in Registry.</p>}
      {!reading && !unreadable && state.available.map((registry) => (
        <Check key={registry.id} label={<span>{registry.name} <span className="registry-help">· {registry.url} · {registry.hasPassword ? 'credentials on file' : 'no stored credentials'}</span></span>} checked={value.includes(registry.id)} disabled={!selectable || !value.includes(registry.id) && value.length >= 32} onChange={(checked) => onChange(checked ? [...value, registry.id] : value.filter((id) => id !== registry.id))} />
      ))}
      {state.unavailable.map((entry) => <div className="registry-unavailable" key={entry.id}><span>{entry.name} — {entry.state === 'denied' ? 'access is not granted' : 'access is unverified'}. This selection cannot authorize credentials.</span><button type="button" className="hud-btn" disabled={disabled} onClick={() => onChange(value.filter((id) => id !== entry.id))}>Remove {entry.name} selection</button></div>)}
      {!state.valid && state.unavailable.length === 0 && <p role="alert">Registry selections must be unique and limited to 32 connections. Remove the invalid selections before accepting work.</p>}
      {value.length > 0 && <button type="button" className="hud-btn" disabled={disabled} onClick={() => onChange([])}>Use anonymous access</button>}
    </fieldset>
  );
}
