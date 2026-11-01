import { useState, type FormEvent } from 'react';
import type { ComposeService, ComposeServicePatch } from '../../types/compose';
import Badge from '../ui/Badge';
import Caps from '../ui/Caps';
import { Actions, Field, FormError, TextArea, TextInput } from '../ui/form';
import { envToLines, parseEnvLines, parseList, sameEnv, sameList } from './composeEdit';

interface Props {
  service: ComposeService;
  busy: boolean;
  /** Rejects with a message when the server refuses the patch. */
  onSave: (patch: ComposeServicePatch) => Promise<void>;
  onClose: () => void;
}

function Row({ label, value }: { label: string; value: string | undefined }) {
  return (
    <>
      <Caps>{label}</Caps>
      <span className={value ? 'v' : 'v muted'}>{value || '—'}</span>
    </>
  );
}

/**
 * The compose service inspector: image, ports and environment are editable
 * (the three fields the service-update endpoint accepts); everything else the
 * parser reports is shown read-only.
 */
export default function ComposeInspector({ service, busy, onSave, onClose }: Props) {
  const [image, setImage] = useState(service.image);
  const [ports, setPorts] = useState(service.ports?.join('\n') ?? '');
  const [env, setEnv] = useState(envToLines(service.environment));
  const [error, setError] = useState<string | null>(null);

  const parsedEnv = parseEnvLines(env);
  const dirty =
    image.trim() !== service.image ||
    !sameList(parseList(ports), service.ports ?? []) ||
    !('env' in parsedEnv && sameEnv(parsedEnv.env, service.environment ?? {}));

  const reset = () => {
    setImage(service.image);
    setPorts(service.ports?.join('\n') ?? '');
    setEnv(envToLines(service.environment));
    setError(null);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if ('error' in parsedEnv) {
      setError(parsedEnv.error);
      return;
    }
    setError(null);
    try {
      await onSave({ image: image.trim(), ports: parseList(ports), environment: parsedEnv.env });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const limits = service.resources ? [service.resources.memory && `mem ${service.resources.memory}`, service.resources.cpus && `cpu ${service.resources.cpus}`].filter(Boolean).join(' · ') : '';

  return (
    <aside className="inspector" aria-label={`Service ${service.name}`}>
      <div className="inspector-head">
        <Badge variant="muted">service</Badge>
        {service.status && service.status !== 'unknown' && <Badge variant={service.status === 'running' ? 'ok' : 'muted'}>{service.status}</Badge>}
        {dirty && <Badge variant="ember">edited</Badge>}
        <span className="spacer" />
        <button type="button" className="inspector-close" onClick={onClose} aria-label="Close inspector">
          ×
        </button>
      </div>
      <h2>{service.name}</h2>
      <form onSubmit={submit} className="inspector-form">
        <Field label="Image" hint={service.build ? `built from ${service.build.context}${service.build.dockerfile ? ` (${service.build.dockerfile})` : ''}` : undefined}>
          <TextInput value={image} onChange={(e) => setImage(e.target.value)} placeholder={service.build ? 'built locally' : 'registry/image:tag'} disabled={busy} />
        </Field>
        <Field label="Ports" hint="one host:container mapping per line">
          <TextArea value={ports} onChange={(e) => setPorts(e.target.value)} rows={3} placeholder="8080:80" disabled={busy} />
        </Field>
        <Field label="Environment" hint="one KEY=value per line">
          <TextArea value={env} onChange={(e) => setEnv(e.target.value)} rows={5} placeholder="KEY=value" disabled={busy} />
        </Field>
        <FormError>{error}</FormError>
        <Actions>
          <button type="submit" className="hud-btn hud-btn-primary" disabled={busy || !dirty}>
            {busy ? 'Saving…' : 'Save'}
          </button>
          <button type="button" className="hud-btn" onClick={reset} disabled={busy || !dirty}>
            Reset
          </button>
        </Actions>
      </form>
      <div className="kv">
        <Row label="Depends on" value={service.dependsOn?.join(', ')} />
        <Row label="Networks" value={service.networks?.join(', ')} />
        <Row label="Volumes" value={service.volumes?.join(', ')} />
        <Row label="Command" value={service.command} />
        <Row label="Group" value={service.group} />
        <Row label="Limits" value={limits} />
      </div>
    </aside>
  );
}
