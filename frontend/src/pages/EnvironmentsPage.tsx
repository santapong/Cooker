import { useEffect, useState } from 'react';
import { useEnvironmentStore } from '../stores/environmentStore';
import type { Environment } from '../types/environment';
import { environmentsApi, type SecretMeta } from '../api/environments';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import {
  Btn,
  Card,
  EmptyState,
  Input,
  Label,
  PageHeader,
  Pill,
  SectionLabel,
} from '../components/ui/atoms';
import { Icon } from '../components/ui/Icon';
import { useToastStore } from '../stores/toastStore';

export default function EnvironmentsPage() {
  const t = useTheme();
  const { environments, loading, fetchEnvironments, createEnvironment } = useEnvironmentStore();
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<Environment | null>(null);

  useEffect(() => {
    fetchEnvironments();
  }, [fetchEnvironments]);

  const seed = async () => {
    setBusy(true);
    try {
      await createEnvironment({
        name: 'dev',
        order: 1,
        target: { type: 'namespace', clusterId: '', namespace: 'cooker-dev', kubeContext: '' },
        promotion: { strategy: 'auto' },
        variables: {},
      });
      await createEnvironment({
        name: 'staging',
        order: 2,
        target: { type: 'namespace', clusterId: '', namespace: 'cooker-staging', kubeContext: '' },
        promotion: { strategy: 'auto' },
        variables: {},
      });
      await createEnvironment({
        name: 'production',
        order: 3,
        target: { type: 'namespace', clusterId: '', namespace: 'cooker-prod', kubeContext: '' },
        promotion: { strategy: 'manual' },
        variables: {},
      });
    } finally {
      setBusy(false);
    }
  };

  const colorFor = (name: string): string => {
    if (name.startsWith('prod')) return t.accent;
    if (name.startsWith('stag')) return t.warn;
    return t.cool;
  };

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="promotion path"
        title="Environments"
        subtitle="The conveyor belt your services move along. Deploys land in the leftmost env first, then promote (auto or manual) to the next. Click an env to manage its secrets."
        actions={<Btn kind="primary" icon="plus">Add environment</Btn>}
      />

      {environments.length === 0 && !loading ? (
        <EmptyState
          title="No environments yet."
          body="Set up dev → staging → production so promotions have somewhere to flow."
          action={
            <Btn kind="primary" icon="spark" onClick={seed} disabled={busy}>
              {busy ? 'Setting up…' : 'Seed dev / staging / prod'}
            </Btn>
          }
        />
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: selected ? '1fr 420px' : '1fr',
            gap: 22,
          }}
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'stretch',
              flexWrap: 'wrap',
              gap: 14,
            }}
          >
            {environments
              .slice()
              .sort((a, b) => a.order - b.order)
              .map((env, i, arr) => {
                const color = colorFor(env.name);
                const isSelected = selected?.id === env.id;
                return (
                  <span
                    key={env.id}
                    style={{ display: 'flex', alignItems: 'center', flex: '1 1 280px' }}
                  >
                    <Card
                      onClick={() => setSelected(isSelected ? null : env)}
                      style={{
                        flex: 1,
                        cursor: 'pointer',
                        borderTop: `3px solid ${color}`,
                        borderColor: hexA(color, 0.4),
                        background: isSelected ? hexA(color, 0.08) : hexA(color, 0.04),
                        boxShadow: isSelected ? `0 0 0 4px ${hexA(color, 0.12)}` : 'none',
                      }}
                    >
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                        <span
                          style={{
                            fontFamily: t.mono,
                            fontSize: 10.5,
                            letterSpacing: 1.4,
                            textTransform: 'uppercase',
                            color,
                            fontWeight: 700,
                          }}
                        >
                          {env.name}
                        </span>
                        <Pill tone={env.promotion.strategy === 'auto' ? 'good' : 'warn'}>
                          {env.promotion.strategy}
                        </Pill>
                      </div>
                      <div
                        style={{
                          fontFamily: t.serif,
                          fontSize: 22,
                          fontWeight: 500,
                          color: t.text,
                          letterSpacing: -0.3,
                          marginBottom: 12,
                        }}
                      >
                        {env.name === 'dev'
                          ? 'Development'
                          : env.name === 'staging'
                            ? 'Staging'
                            : env.name === 'production'
                              ? 'Production'
                              : env.name}
                      </div>
                      <Detail
                        label="Target"
                        value={
                          env.target.type === 'namespace'
                            ? `ns/${env.target.namespace}`
                            : `cluster/${env.target.clusterId}`
                        }
                      />
                      <Detail
                        label="Variables"
                        value={`${Object.keys(env.variables).length} keys`}
                      />
                    </Card>
                    {i < arr.length - 1 && (
                      <div
                        style={{
                          display: 'flex',
                          flexDirection: 'column',
                          alignItems: 'center',
                          color: t.textMute,
                          padding: '0 12px',
                          gap: 4,
                        }}
                      >
                        <Icon name="arrow" size={20} />
                        <span
                          style={{
                            fontFamily: t.mono,
                            fontSize: 9.5,
                            letterSpacing: 0.8,
                            textTransform: 'uppercase',
                          }}
                        >
                          promote
                        </span>
                      </div>
                    )}
                  </span>
                );
              })}
          </div>

          {selected && <SecretsPanel env={selected} environments={environments} onClose={() => setSelected(null)} />}
        </div>
      )}
    </div>
  );
}

function SecretsPanel({
  env,
  environments,
  onClose,
}: {
  env: Environment;
  environments: Environment[];
  onClose: () => void;
}) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);

  // Backend doesn't expose a list endpoint for secrets — only PUT /
  // DELETE / GET-by-key / promote. So we render the env's known
  // `variables` (non-secret) and a simple add-secret form. Operators
  // who need to recall an existing key paste it in to reveal.
  const [secrets, setSecrets] = useState<SecretMeta[]>([]);
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');
  const [busy, setBusy] = useState(false);
  const [revealKey, setRevealKey] = useState('');
  const [revealed, setRevealed] = useState<{ key: string; value: string } | null>(null);
  const [promoteTo, setPromoteTo] = useState<string>('');

  const otherEnvs = environments.filter((e) => e.id !== env.id);

  useEffect(() => {
    // Best-effort: track add/delete in local state since the backend
    // doesn't list secret keys.
    setSecrets([]);
    setRevealed(null);
    setRevealKey('');
  }, [env.id]);

  const addSecret = async () => {
    if (!newKey || !newValue) return;
    setBusy(true);
    try {
      await environmentsApi.putSecret(env.id, newKey, newValue);
      setSecrets((s) => [...s.filter((x) => x.key !== newKey), { key: newKey, updatedAt: new Date().toISOString() }]);
      setNewKey('');
      setNewValue('');
      pushToast({ kind: 'success', message: `Secret "${newKey}" saved.` });
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  const reveal = async () => {
    if (!revealKey) return;
    setBusy(true);
    try {
      const { value } = await environmentsApi.revealSecret(env.id, revealKey);
      setRevealed({ key: revealKey, value });
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  const removeSecret = async (key: string) => {
    if (!confirm(`Delete secret "${key}"?`)) return;
    try {
      await environmentsApi.deleteSecret(env.id, key);
      setSecrets((s) => s.filter((x) => x.key !== key));
      if (revealed?.key === key) setRevealed(null);
      pushToast({ kind: 'success', message: `Secret "${key}" deleted.` });
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  const promote = async () => {
    if (!promoteTo) return;
    setBusy(true);
    try {
      const { promoted } = await environmentsApi.promoteSecrets(env.id, promoteTo);
      pushToast({
        kind: 'success',
        message: `Promoted ${promoted?.length ?? 0} secrets to ${promoteTo}.`,
      });
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card pad={0}>
      <div
        style={{
          padding: '14px 18px',
          borderBottom: `1px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}
      >
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text, flex: 1 }}>
          {env.name} secrets
        </span>
        <Pill tone="warn">admin · MFA</Pill>
        <button
          onClick={onClose}
          style={{
            background: 'transparent',
            border: `1px solid ${t.line}`,
            color: t.textSoft,
            width: 28,
            height: 28,
            borderRadius: 6,
            display: 'grid',
            placeItems: 'center',
            cursor: 'pointer',
          }}
        >
          <Icon name="close" size={12} />
        </button>
      </div>

      <div style={{ padding: 18, display: 'flex', flexDirection: 'column', gap: 18 }}>
        <div>
          <SectionLabel>Add or update</SectionLabel>
          <Label>Key</Label>
          <Input
            value={newKey}
            onChange={(e) => setNewKey(e.target.value)}
            placeholder="DATABASE_URL"
          />
          <Label>Value</Label>
          <Input
            type="password"
            value={newValue}
            onChange={(e) => setNewValue(e.target.value)}
            placeholder="••••••••"
          />
          <Btn
            kind="primary"
            icon="check"
            onClick={addSecret}
            disabled={busy || !newKey || !newValue}
            style={{ marginTop: 12, width: '100%', justifyContent: 'center' }}
          >
            {busy ? 'Saving…' : 'Save secret'}
          </Btn>
        </div>

        <div>
          <SectionLabel>Reveal an existing secret</SectionLabel>
          <Label>Key</Label>
          <Input
            value={revealKey}
            onChange={(e) => setRevealKey(e.target.value)}
            placeholder="DATABASE_URL"
          />
          <Btn
            kind="secondary"
            onClick={reveal}
            disabled={busy || !revealKey}
            style={{ marginTop: 12, width: '100%', justifyContent: 'center' }}
          >
            Reveal
          </Btn>
          {revealed && (
            <div
              style={{
                marginTop: 12,
                padding: 12,
                background: t.bg,
                border: `1px solid ${t.line}`,
                borderRadius: 8,
                fontFamily: t.mono,
                fontSize: 12,
                wordBreak: 'break-all',
              }}
            >
              <div style={{ color: t.textMute, marginBottom: 4 }}>{revealed.key}</div>
              <div style={{ color: t.text }}>{revealed.value}</div>
              <div
                style={{
                  display: 'flex',
                  gap: 8,
                  marginTop: 10,
                  justifyContent: 'flex-end',
                }}
              >
                <Btn kind="ghost" onClick={() => setRevealed(null)}>
                  Hide
                </Btn>
                <Btn kind="danger" onClick={() => removeSecret(revealed.key)}>
                  Delete
                </Btn>
              </div>
            </div>
          )}
        </div>

        {secrets.length > 0 && (
          <div>
            <SectionLabel>This session</SectionLabel>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 8 }}>
              {secrets.map((s) => (
                <div
                  key={s.key}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    padding: '8px 10px',
                    border: `1px solid ${t.line}`,
                    borderRadius: 7,
                    background: t.bg,
                    fontFamily: t.mono,
                    fontSize: 12,
                  }}
                >
                  <Icon name="flask" size={12} style={{ color: t.textMute }} />
                  <span style={{ flex: 1, color: t.text }}>{s.key}</span>
                  <span style={{ fontSize: 10, color: t.textMute }}>just saved</span>
                  <Btn kind="ghost" onClick={() => removeSecret(s.key)}>
                    Delete
                  </Btn>
                </div>
              ))}
            </div>
          </div>
        )}

        {otherEnvs.length > 0 && (
          <div>
            <SectionLabel>Promote secrets</SectionLabel>
            <Label>To environment</Label>
            <select
              value={promoteTo}
              onChange={(e) => setPromoteTo(e.target.value)}
              style={{
                width: '100%',
                padding: '9px 11px',
                background: t.bg,
                color: t.text,
                border: `1px solid ${t.line}`,
                borderRadius: 7,
                fontSize: 13.5,
                fontFamily: t.sans,
                outline: 'none',
              }}
            >
              <option value="">Select an environment…</option>
              {otherEnvs.map((e) => (
                <option key={e.id} value={e.id}>
                  {e.name}
                </option>
              ))}
            </select>
            <Btn
              kind="ink"
              icon="arrow"
              onClick={promote}
              disabled={busy || !promoteTo}
              style={{ marginTop: 12, width: '100%', justifyContent: 'center' }}
            >
              Copy all secrets to selected env
            </Btn>
          </div>
        )}
      </div>
    </Card>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  const t = useTheme();
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12.5, marginTop: 4 }}>
      <span style={{ color: t.textMute }}>{label}</span>
      <span style={{ fontFamily: t.mono, color: t.text }}>{value}</span>
    </div>
  );
}
