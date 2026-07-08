import { useEffect, useState } from 'react';
import type { Environment } from '../../types/environment';
import { environmentsApi, type SecretMeta } from '../../api/environments';
import { useTheme } from '../../theme/ThemeProvider';
import { Btn, Card, Input, Label, Pill, SectionLabel } from '../ui/atoms';
import { Icon } from '../ui/Icon';
import { useToastStore } from '../../stores/toastStore';

interface EnvironmentEditorProps {
  env: Environment;
  environments: Environment[];
  onClose: () => void;
}

/**
 * Secrets editor for a selected environment — add/reveal/delete secrets and
 * promote them to another environment.
 *
 * The backend doesn't expose a list endpoint for secrets — only PUT /
 * DELETE / GET-by-key / promote. So this renders the env's add-secret form
 * and a simple session-local record of what's been added; operators who
 * need to recall an existing key paste it in to reveal.
 */
export default function EnvironmentEditor({ env, environments, onClose }: EnvironmentEditorProps) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);

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
