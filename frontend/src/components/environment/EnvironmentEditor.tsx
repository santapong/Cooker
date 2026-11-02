import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { useAuth } from '../../auth/OIDCProvider';
import { environmentsApi, type SecretKeysMetadata } from '../../api/environments';
import type { Environment } from '../../types/environment';
import { useConfigurationDraftStore } from '../../stores/configurationDraftStore';
import { configurationError, configurationText, environmentKeyPattern, ordinaryValues, parseConfiguration } from '../../utils/environmentEditing';
import { Actions, Field, TextArea, TextInput } from '../ui/form';
import ConfirmButton from '../ui/ConfirmButton';
import { useDraftMutationFence } from './useDraftMutationFence';
import './environment-editing.css';

interface ConfigDraft { base: Environment; text: string }
interface SecretDraft { key: string; value: string; generation?: number }
export default function EnvironmentEditor({ environment, onSaved }: { environment: Environment; onSaved: () => Promise<void> }) {
  const { user } = useAuth();
  const owner = user?.sub ?? '';
  const canEdit = !!user?.roles.some((role) => role === 'operator' || role === 'admin');
  const admin = !!user?.roles.includes('admin');
  const drafts = useConfigurationDraftStore();
  const currentMutation = useDraftMutationFence();
  const configKey = `environment:${environment.id}:configuration`;
  const secretKey = `environment:${environment.id}:protected`;
  const config = (drafts.owner === owner ? drafts.drafts[configKey] : undefined) as ConfigDraft | undefined;
  const secret = (drafts.owner === owner ? drafts.drafts[secretKey] : undefined) as SecretDraft | undefined;
  const secretForm = secret ?? { key: '', value: '' };
  const [metadata, setMetadata] = useState<SecretKeysMetadata | null>(null);
  const [metadataUnavailable, setMetadataUnavailable] = useState(false);
  const metadataRequest = useRef(0);
  const refreshMetadata = useCallback(async () => {
    const request = ++metadataRequest.current;
    try {
      const result = await environmentsApi.secretKeys(environment.id);
      if (request !== metadataRequest.current) return;
      if (!Array.isArray(result.keys) || !result.keys.every((key) => typeof key === 'string') || !Number.isSafeInteger(result.generation) || result.generation < 0) throw new Error('Invalid metadata');
      setMetadata(result); setMetadataUnavailable(result.sourceAvailability !== 'available');
    } catch { if (request === metadataRequest.current) { setMetadata(null); setMetadataUnavailable(true); } }
  }, [environment.id]);
  const invalidateMetadata = useCallback(() => { metadataRequest.current++; }, []);
  useEffect(() => { setMetadata(null); setMetadataUnavailable(false); void refreshMetadata(); return invalidateMetadata; }, [refreshMetadata, environment.version, invalidateMetadata]);
  const protectedKeys = metadata?.sourceAvailability === 'available' ? metadata.keys : environment.secretKeys ?? [];
  const conditionalEditing = !metadataUnavailable && metadata?.sourceAvailability === 'available' && metadata.conditionalEditing === true;
  const secretStale = !!secret && secret.generation !== metadata?.generation;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  useEffect(() => { setBusy(false); setError(null); setNotice(null); }, [currentMutation]);
  const stale = !!config && config.base.version !== environment.version;
  const configText = config?.text ?? configurationText(environment);
  const setSecret = (patch: Partial<SecretDraft>) => {
    if (admin && !busy) drafts.setDraft(owner, secretKey, { ...secretForm, generation: secret?.generation ?? metadata?.generation, ...patch });
  };
  const saveConfiguration = async (event: FormEvent) => {
    event.preventDefault();
    if (!canEdit || !config || stale || busy || config.base.version === undefined) return;
    let plainVars: Record<string, string>;
    try { plainVars = parseConfiguration(config.text, protectedKeys); }
    catch (failure) { setError(failure instanceof Error ? failure.message : 'Review the configuration keys.'); return; }
    const generation = drafts.generation;
    setBusy(true); setError(null); setNotice(null);
    try {
      await environmentsApi.update(environment.id, { ...config.base, plainVars, variables: undefined });
      drafts.clearDraftIf(owner, configKey, config, generation);
      if (!currentMutation(generation)) return;
      setNotice('Readable configuration saved. Existing protected values were kept.');
      await Promise.all([onSaved(), refreshMetadata()]);
    } catch (failure) { if (currentMutation(generation)) { setError(configurationError(failure)); void refreshMetadata(); } }
    finally { if (currentMutation(generation)) setBusy(false); }
  };
  const storeSecret = async (event: FormEvent) => {
    event.preventDefault();
    if (!admin || busy || !conditionalEditing || secretStale || secret?.generation === undefined) return;
    const key = secretForm.key.trim();
    if (!environmentKeyPattern.test(key) || !secretForm.value) { setError('Enter a valid key name and a new protected value.'); return; }
    if (Object.prototype.hasOwnProperty.call(ordinaryValues(environment), key)) { setError('This key is readable configuration. Remove or rename its readable entry before protecting it.'); return; }
    const generation = drafts.generation;
    setBusy(true); setError(null); setNotice(null);
    try {
      await environmentsApi.putSecret(environment.id, key, secretForm.value, secret.generation);
      drafts.clearDraftIf(owner, secretKey, secret, generation);
      if (!currentMutation(generation)) return;
      setNotice('Protected value stored. Its value is not returned or read by this editor.');
      await Promise.all([onSaved(), refreshMetadata()]);
    } catch (failure) { if (currentMutation(generation)) { setError(configurationError(failure)); void refreshMetadata(); } }
    finally { if (currentMutation(generation)) setBusy(false); }
  };
  const removeSecret = async (key: string) => {
    if (!admin || busy || !conditionalEditing || metadata?.generation === undefined || !environmentKeyPattern.test(key)) return;
    const generation = drafts.generation;
    setBusy(true); setError(null); setNotice(null);
    try {
      await environmentsApi.deleteSecret(environment.id, key, metadata.generation);
      if (!currentMutation(generation)) return;
      setNotice('Protected key removal acknowledged. Review affected service bindings before deploying.');
      await Promise.all([onSaved(), refreshMetadata()]);
    } catch (failure) { if (currentMutation(generation)) { setError(configurationError(failure)); void refreshMetadata(); } }
    finally { if (currentMutation(generation)) setBusy(false); }
  };
  return <section className="environment-editor panel" aria-label={`${environment.name} configuration and protected values`}>
    <h2>{environment.name}</h2>
    <p className="workspace-note">Saved version {environment.version ?? 'unavailable'}. Readable settings and protected values use separate controls. Saving does not deploy.</p>
    {error && <p role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}
    <form onSubmit={(event) => void saveConfiguration(event)}>
      <h3>Readable configuration</h3>
      <Field label="Ordinary configuration" hint="One KEY=value per line, or a JSON object for multiline values. These values are readable by authenticated users. Passwords and tokens belong below."><TextArea value={configText} rows={6} maxLength={65536} readOnly={!canEdit} onChange={(event) => { if (canEdit && !busy) drafts.setDraft(owner, configKey, { base: config?.base ?? environment, text: event.target.value }); }} /></Field>
      {stale && <p role="alert">The saved environment changed while you were editing. Your draft is retained; compare the latest configuration before discarding or reapplying your changes.</p>}
      {config && <details><summary>Latest saved readable configuration</summary><pre className="environment-saved-config">{configurationText(environment) || 'No readable values'}</pre></details>}
      {canEdit && <Actions><button type="submit" className="hud-btn hud-btn-primary" disabled={busy || !config || stale || config.base.version === undefined}>Save configuration</button>{config && <ConfirmButton className="hud-btn" disabled={busy} confirmLabel="Discard this configuration draft?" onConfirm={() => { drafts.clearDraft(owner, configKey); setError(null); }}>Discard draft</ConfirmButton>}{stale && <ConfirmButton className="hud-btn" disabled={busy} confirmLabel="Apply this draft against the latest saved version?" onConfirm={() => drafts.setDraft(owner, configKey, { base: environment, text: config.text })}>Use latest configuration version</ConfirmButton>}<button type="button" className="hud-btn" disabled={busy} onClick={() => { void onSaved(); void refreshMetadata(); }}>Refresh saved data</button></Actions>}
      {canEdit && environment.version === undefined && <p role="alert">A saved version is required before editing configuration. Refresh saved data.</p>}
    </form>
    <h3>Protected values</h3>
    <p className="workspace-note">Values stay masked and are never fetched here. Only administrators can add, replace or remove them; the server also enforces configured MFA. External providers may not report all key names.</p>
    <div className="environment-secret-list">{protectedKeys.map((key) => <div key={key} className="environment-secret-row"><code>{key}</code><span aria-label="Protected value hidden">••••••••</span>{admin && <ConfirmButton className="hud-btn" disabled={busy || !conditionalEditing} confirmLabel={`Remove ${key}?`} onConfirm={() => removeSecret(key)}>Remove protected key</ConfirmButton>}</div>)}</div>
    {!protectedKeys.length && <p>No protected key names are reported. This is not proof that an external provider has no values.</p>}
    {metadataUnavailable && <p role="alert">Protected key inventory is unavailable. Reported saved names may be incomplete; protected writes are disabled.</p>}
    {metadata && !metadata.conditionalEditing && <p role="alert">This protected-value backend does not support version-checked editing. Use its own management tools; this editor cannot safely replace or remove values.</p>}
    {!metadata && !metadataUnavailable && <p role="status">Checking protected key metadata…</p>}
    {secretStale && <p role="alert">The protected-value generation changed or is not confirmed. Your masked draft is retained. Refresh and compare key names before choosing a new version.</p>}
    {admin && <form onSubmit={(event) => void storeSecret(event)}><fieldset disabled={busy}>
      <Field label="Protected key name"><TextInput value={secretForm.key} maxLength={256} autoComplete="off" onChange={(event) => setSecret({ key: event.target.value })} placeholder="DATABASE_PASSWORD" /></Field>
      <Field label="New protected value" hint="Write-only in this editor. An unsuccessful save keeps this masked draft in session memory."><TextInput type="password" value={secretForm.value} maxLength={16384} autoComplete="new-password" onChange={(event) => setSecret({ value: event.target.value })} /></Field>
      <p className="workspace-note">Replacing a protected value is an explicit, version-checked write. Existing values are never revealed.</p>
      <Actions><button type="submit" className="hud-btn hud-btn-primary" disabled={!secretForm.key.trim() || !secretForm.value || !conditionalEditing || secretStale}>Store protected value</button>{secret && <ConfirmButton className="hud-btn" confirmLabel="Discard this protected draft?" onConfirm={() => drafts.clearDraft(owner, secretKey)}>Discard protected draft</ConfirmButton>}{secretStale && conditionalEditing && <ConfirmButton className="hud-btn" confirmLabel="Use the latest generation for this masked draft?" onConfirm={() => drafts.setDraft(owner, secretKey, { ...secretForm, generation: metadata?.generation })}>Use latest protected generation</ConfirmButton>}{secretForm.key.trim() && <ConfirmButton className="hud-btn" disabled={!conditionalEditing} confirmLabel={`Remove ${secretForm.key.trim()}?`} onConfirm={() => removeSecret(secretForm.key.trim())}>Remove named protected key</ConfirmButton>}</Actions>
    </fieldset></form>}
    {!canEdit && <p>Configuration is read-only for your role.</p>}
    <p className="workspace-note">Unsaved drafts stay in this signed-in browser's memory during navigation and theme changes. A page reload, sign-out or explicit discard clears them. No drafts are written to local or session storage.</p>
  </section>;
}
