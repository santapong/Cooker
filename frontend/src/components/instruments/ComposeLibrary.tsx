import { useEffect, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import type { ComposeGraph } from '../../types/compose';
import { useComposeLibraryStore } from '../../stores/composeLibraryStore';
import { pushToast } from '../../stores/toastStore';
import { ComposeIcon, RegistryIcon } from '../icons';
import { Field, FormError, TextInput } from '../ui/form';
import ConfirmButton from '../ui/ConfirmButton';
import GitHubStacks from './GitHubStacks';

interface Props {
  graph: ComposeGraph | null;
  composePath: string;
  busy: boolean;
  onOpen: (composePath: string) => void;
}

export default function ComposeLibrary({ graph, composePath, busy, onOpen }: Props) {
  const { stacks, error, hydrate, saveStack, forgetStack } = useComposeLibraryStore();
  const [query, setQuery] = useState('');
  const current = stacks.find((s) => s.composePath === composePath);
  const [name, setName] = useState('');
  useEffect(() => { hydrate(); }, [hydrate]);
  useEffect(() => { setName(current?.name ?? composePath.replace(/\.ya?ml$/i, '')); }, [composePath, current?.name]);

  const save = (event: FormEvent) => {
    event.preventDefault();
    if (!graph || busy) return;
    if (saveStack(name, composePath, graph)) pushToast('success', `“${name.trim()}” saved to this browser's Compose registry.`);
  };
  const filtered = stacks.filter((s) => `${s.name} ${s.composePath}`.toLowerCase().includes(query.trim().toLowerCase()));

  return (
    <aside className="compose-library" aria-label="Compose registry">
      <div className="compose-library-heading">
        <ComposeIcon />
        <div><h2>Compose registry</h2><span className="compose-muted">Saved stacks · this browser</span></div>
        <span className="compose-count mono">{stacks.length}</span>
      </div>
      <TextInput aria-label="Search saved stacks" type="search" placeholder="Find a stack…" value={query} onChange={(e) => setQuery(e.target.value)} />
      <GitHubStacks query={query} />
      <FormError>{error}</FormError>
      <div className="compose-stack-list">
        {filtered.length === 0 && (
          <div className="compose-library-empty">
            <ComposeIcon width={32} height={32} />
            <strong>{stacks.length ? 'No matching stacks' : 'Your stacks, ready to reopen'}</strong>
            <p>{stacks.length ? 'Try another name or filename.' : 'Open a Compose file, then save it here with a name you recognise.'}</p>
          </div>
        )}
        {filtered.map((stack) => (
          <article key={stack.composePath} className={`compose-stack${graph && composePath === stack.composePath ? ' is-current' : ''}`}>
            <button type="button" className="compose-stack-open" aria-label={`Open ${stack.name}`} disabled={busy} onClick={() => onOpen(stack.composePath)}>
              <span className="compose-stack-title"><ComposeIcon /><strong>{stack.name}</strong>{graph && composePath === stack.composePath && <span className="compose-current">Open</span>}</span>
              <span className="mono compose-stack-file">{stack.composePath}</span>
              <span className="compose-stack-counts">{stack.services} services · {stack.networks} networks · {stack.volumes} volumes</span>
            </button>
            <div className="compose-stack-foot">
              <span title={new Date(stack.checkedAt).toLocaleString()}>Saved {new Date(stack.checkedAt).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}</span>
              <ConfirmButton disabled={busy} onConfirm={() => {
                if (forgetStack(stack.composePath)) pushToast('info', `“${stack.name}” removed from the library. The Compose file is unchanged.`);
              }}>Forget</ConfirmButton>
            </div>
          </article>
        ))}
      </div>
      {graph && (
        <form className="compose-library-save" onSubmit={save}>
          <Field label="Stack name"><TextInput value={name} onChange={(e) => setName(e.target.value)} required maxLength={80} disabled={busy} /></Field>
          <button type="submit" className="hud-btn hud-btn-primary" disabled={busy || !name.trim()}>{current ? 'Update saved stack' : 'Save stack'}</button>
          <p className="compose-muted">Saves this file reference and its counts. Reopening reads the file again.</p>
        </form>
      )}
      <Link to="/registry" className="compose-registry-link"><RegistryIcon /> Image registries <span aria-hidden="true">↗</span></Link>
    </aside>
  );
}
