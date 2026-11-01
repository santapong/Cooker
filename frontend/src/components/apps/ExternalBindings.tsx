import type { ExternalServiceBinding } from '../../types/app';
import type { ComposeService } from '../../types/compose';
import { Check, Field, Select, TextArea, TextInput } from '../ui/form';

export default function ExternalBindings({ services, bindings, onChange }: {
  services: ComposeService[]; bindings: ExternalServiceBinding[]; onChange: (bindings: ExternalServiceBinding[]) => void;
}) {
  const update = (service: string, patch: Partial<ExternalServiceBinding>) => onChange(bindings.map((b) => b.service === service ? { ...b, ...patch } : b));
  return <div className="external-bindings">{services.map((service) => {
    const binding = bindings.find((b) => b.service === service.name);
    return <div className="external-binding" key={service.name}>
      <h3>{service.name}</h3>
      <Check label="Use an existing database" checked={!!binding} onChange={(enabled) => {
        if (!enabled) { onChange(bindings.filter((b) => b.service !== service.name)); return; }
        onChange([...bindings, { service: service.name, provider: 'gcp-cloud-sql', resource: '', consumers: services.filter((s) => s.name !== service.name && !bindings.some((b) => b.service === s.name) && s.dependsOn?.includes(service.name)).map((s) => s.name), environment: { DATABASE_URL: 'DATABASE_URL' } }]);
      }} />
      {binding && <>
        <div className="panel-grid">
          <Field label="Database provider"><Select value={binding.provider} onChange={(e) => update(service.name, { provider: e.target.value as ExternalServiceBinding['provider'] })} options={[{ value: 'gcp-cloud-sql', label: 'GCP Cloud SQL' }, { value: 'external', label: 'Other existing database' }]} /></Field>
          <Field label="Existing resource" hint="The resource name. Connection values come from your environment."><TextInput value={binding.resource} placeholder="project:region:instance" onChange={(e) => update(service.name, { resource: e.target.value })} /></Field>
        </div>
        <fieldset className="binding-consumers"><legend>Services using this database</legend>
          {services.filter((s) => s.name !== service.name && !bindings.some((b) => b.service === s.name)).map((s) => <Check key={s.name} label={s.name} checked={binding.consumers.includes(s.name)} onChange={(checked) => update(service.name, { consumers: checked ? [...binding.consumers, s.name] : binding.consumers.filter((name) => name !== s.name) })} />)}
        </fieldset>
        <Field label="Connection key bindings" hint="One APP_VARIABLE=ENVIRONMENT_KEY per line. Enter key names only.">
          <TextArea rows={3} defaultValue={Object.entries(binding.environment).map(([key, value]) => `${key}=${value}`).join('\n')} onChange={(e) => {
            const pairs = e.target.value.split('\n').map((line) => line.trim()).filter(Boolean).map((line) => { const split = line.indexOf('='); return split < 0 ? [line, ''] : [line.slice(0, split).trim(), line.slice(split + 1).trim()]; });
            update(service.name, { environment: Object.fromEntries(pairs) });
          }} />
        </Field>
      </>}
    </div>;
  })}</div>;
}
