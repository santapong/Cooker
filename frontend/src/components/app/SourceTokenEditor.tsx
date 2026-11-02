import { useState, type FormEvent } from 'react';
import { ApiError } from '../../api/client';
import { automationApi, validSourceConnectionMetadata, type SourceConnection } from '../../api/automation';
import { useConfigurationDraftStore } from '../../stores/configurationDraftStore';
import { useDraftMutationFence } from '../environment/useDraftMutationFence';
import { Actions, Field, FormError, TextInput } from '../ui/form';

export default function SourceTokenEditor({ connection, onSaved, onClose, onReload }: {
  connection: SourceConnection;
  onSaved: (connection: SourceConnection, checkedRepositories: number) => void;
  onClose: () => void;
  onReload: () => Promise<void>;
}) {
  const [token, setToken] = useState('');
  const [username, setUsername] = useState(connection.username ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [conflict, setConflict] = useState(false);
  const current = useDraftMutationFence();

  async function save(event: FormEvent) {
    event.preventDefault();
    if (busy || conflict) return;
    const generation = useConfigurationDraftStore.getState().generation;
    setBusy(true); setError('');
    try {
      const result = await automationApi.replaceToken(connection.id, { version: connection.version, token, username });
      if (!current(generation)) return;
      if (!validSourceConnectionMetadata(result.connection) || result.connection.id !== connection.id || result.connection.version !== connection.version + 1 || result.check !== 'repository_metadata_access' || result.checkedRepositories !== connection.repositories.length || result.checkedRepositories !== result.connection.repositories.length) throw new Error('Replacement acknowledgement unavailable');
      setToken('');
      onSaved(result.connection, result.checkedRepositories);
    } catch (cause) {
      if (!current(generation)) return;
      const status = cause instanceof ApiError ? cause.status : undefined;
      if (status === 409) {
        setConflict(true);
        setError('This connection changed. Reload its settings before checking another replacement.');
      } else if (status === undefined || status >= 500) {
        setConflict(true);
        setError('The replacement result could not be confirmed. Reload this connection before trying again.');
      } else if (status === 403) {
        setError('An administrator with the required authentication must replace this token.');
      } else {
        setError('The replacement was not saved. Check the token, Git username and access to every allowed repository.');
      }
    } finally {
      if (current(generation)) setBusy(false);
    }
  }

  async function reload() {
    const generation = useConfigurationDraftStore.getState().generation;
    setBusy(true); setError('');
    try {
      await onReload();
      if (current(generation)) { setToken(''); onClose(); }
    } catch {
      if (current(generation)) setError('Could not reload this connection. Try reloading before another replacement.');
    } finally {
      if (current(generation)) setBusy(false);
    }
  }

  return <form onSubmit={save} aria-label={`Replace token for ${connection.name}`}>
    <p>Cooker checks repository metadata access for all {connection.repositories.length} allowed repositories before saving. The webhook secret and server address stay the same. This check does not build or deploy.</p>
    <Field label="Replacement repository token" hint="The saved token is never displayed. This draft is cleared when you close or leave this page.">
      <TextInput type="password" autoComplete="new-password" required disabled={busy || conflict} value={token} onChange={(event) => setToken(event.target.value)} />
    </Field>
    <Field label="Git username" hint={connection.provider === 'gitea' ? 'The account username for this token.' : 'Use the provider’s Git username; the saved default is shown.'}>
      <TextInput required={connection.provider === 'gitea'} disabled={busy || conflict} value={username} onChange={(event) => setUsername(event.target.value)} />
    </Field>
    <FormError>{error}</FormError>
    <Actions>
      {conflict ? <button type="button" className="hud-btn" disabled={busy} onClick={() => void reload()}>Reload connection and clear draft</button> : <button className="hud-btn" disabled={busy || !token}>{busy ? 'Checking repositories…' : 'Check and replace token'}</button>}
      <button type="button" className="hud-btn" disabled={busy} onClick={() => { setToken(''); onClose(); }}>Cancel</button>
    </Actions>
  </form>;
}
