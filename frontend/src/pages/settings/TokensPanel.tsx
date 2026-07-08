import { useEffect, useRef, useState } from 'react';
import { tokensApi } from '../../api/tokens';
import type { APIToken } from '../../types/token';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Btn, Card, EmptyState, Input, Label, Pill, Select } from '../../components/ui/atoms';
import { DataTable } from '../../components/ui/DataTable';
import { useToastStore } from '../../stores/toastStore';
import { useAuth } from '../../auth/OIDCProvider';
import { SectionHeader } from './SectionHeader';

// ── Role helpers ─────────────────────────────────────────────────────────────

const ALL_ROLES = ['admin', 'operator', 'approver', 'viewer'] as const;
type Role = (typeof ALL_ROLES)[number];

function mintableRoles(userRoles: string[]): Role[] {
  if (userRoles.includes('admin')) return [...ALL_ROLES];
  if (userRoles.includes('operator')) return ['operator', 'viewer'];
  if (userRoles.includes('approver')) return ['approver', 'viewer'];
  return ['viewer'];
}

function roleTone(role: string): 'accent' | 'good' | 'warn' | 'cool' | 'neutral' {
  switch (role) {
    case 'admin':
      return 'accent';
    case 'operator':
      return 'good';
    case 'approver':
      return 'warn';
    case 'viewer':
      return 'cool';
    default:
      return 'neutral';
  }
}

function fmtDate(iso?: string): string {
  if (!iso) return 'never';
  try {
    return new Date(iso).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });
  } catch {
    return iso;
  }
}

// ── Tokens panel ──────────────────────────────────────────────────────────────

export default function TokensPanel() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const { user } = useAuth();
  const [items, setItems] = useState<APIToken[]>([]);
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);
  const [newTokenPlaintext, setNewTokenPlaintext] = useState<string | null>(null);

  const roles = mintableRoles(user?.roles ?? []);

  const refresh = () => {
    setLoading(true);
    tokensApi
      .list()
      .then(setItems)
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  const revoke = async (id: string, name: string) => {
    if (!window.confirm(`Revoke token "${name}"? This cannot be undone.`)) return;
    try {
      await tokensApi.remove(id);
      pushToast({ kind: 'success', message: `Token "${name}" revoked.` });
      refresh();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  const handleCreated = (plaintext: string) => {
    setAdding(false);
    setNewTokenPlaintext(plaintext);
  };

  const handleModalClose = () => {
    setNewTokenPlaintext(null);
    refresh();
  };

  return (
    <>
      {newTokenPlaintext && (
        <TokenCreatedModal token={newTokenPlaintext} onClose={handleModalClose} />
      )}
      <div style={{ display: 'grid', gridTemplateColumns: adding ? '1fr 360px' : '1fr', gap: 22 }}>
        <Card pad={0}>
          <SectionHeader
            title="API tokens"
            count={items.length}
            action={
              <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
                Create token
              </Btn>
            }
          />
          {loading ? (
            <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
          ) : items.length === 0 ? (
            <EmptyState
              title="No API tokens."
              body="Create a personal access token or service-account token to authenticate CI/CD pipelines and scripts against the Cooker API."
              action={
                <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
                  Create the first token
                </Btn>
              }
            />
          ) : (
            <DataTable
              rows={items}
              rowKey={(r) => r.id}
              columns={[
                {
                  key: 'name',
                  header: 'Name',
                  render: (r) => (
                    <span
                      style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}
                    >
                      {r.name}
                    </span>
                  ),
                },
                {
                  key: 'prefix',
                  header: 'Prefix',
                  width: '160px',
                  render: (r) => (
                    <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                      {r.displayPrefix}
                    </span>
                  ),
                },
                {
                  key: 'role',
                  header: 'Role',
                  width: '120px',
                  render: (r) => <Pill tone={roleTone(r.role)}>{r.role}</Pill>,
                },
                {
                  key: 'lastUsed',
                  header: 'Last used',
                  width: '130px',
                  render: (r) => (
                    <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute }}>
                      {fmtDate(r.lastUsedAt)}
                    </span>
                  ),
                },
                {
                  key: 'expiry',
                  header: 'Expires',
                  width: '130px',
                  render: (r) => (
                    <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute }}>
                      {fmtDate(r.expiresAt)}
                    </span>
                  ),
                },
                {
                  key: 'actions',
                  header: '',
                  width: '90px',
                  align: 'right',
                  render: (r) => (
                    <Btn kind="danger" onClick={() => revoke(r.id, r.name)}>
                      Revoke
                    </Btn>
                  ),
                },
              ]}
            />
          )}
        </Card>

        {adding && (
          <AddTokenPanel
            availableRoles={roles}
            onCancel={() => setAdding(false)}
            onCreated={handleCreated}
          />
        )}
      </div>
    </>
  );
}

// ── Show-once modal ───────────────────────────────────────────────────────────

function TokenCreatedModal({ token, onClose }: { token: string; onClose: () => void }) {
  const t = useTheme();
  const [copied, setCopied] = useState(false);
  // Store the reset timer so it can be cancelled on unmount and avoid a
  // setState-after-unmount warning (FE-M SettingsPage).
  const copyTimerRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (copyTimerRef.current !== null) {
        window.clearTimeout(copyTimerRef.current);
      }
    };
  }, []);

  const copy = () => {
    navigator.clipboard.writeText(token).then(
      () => setCopied(true),
      () => {},
    );
    if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
    copyTimerRef.current = window.setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="token-modal-title"
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 1000,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: hexA('#000', 0.6),
        backdropFilter: 'blur(4px)',
        WebkitBackdropFilter: 'blur(4px)',
      }}
    >
      <Card
        pad={0}
        style={{ width: '100%', maxWidth: 520, margin: '0 16px' }}
      >
        <div
          style={{
            padding: '14px 18px',
            borderBottom: `1px solid ${t.line}`,
            background: hexA(t.good, 0.06),
          }}
        >
          <span
            id="token-modal-title"
            style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}
          >
            Token created
          </span>
        </div>
        <div style={{ padding: 18, display: 'flex', flexDirection: 'column', gap: 14 }}>
          <p
            style={{
              fontFamily: t.sans,
              fontSize: 13,
              color: t.bad,
              margin: 0,
              padding: '10px 12px',
              background: hexA(t.bad, 0.07),
              border: `1px solid ${hexA(t.bad, 0.3)}`,
              borderRadius: 8,
              lineHeight: 1.55,
            }}
          >
            Copy this token now. It will <strong>not</strong> be shown again after you close this
            dialog.
          </p>
          <div
            style={{
              fontFamily: t.mono,
              fontSize: 13,
              color: t.text,
              background: hexA(t.surfaceSolid, 0.5),
              border: `1px solid ${t.line}`,
              borderRadius: 8,
              padding: '12px 14px',
              wordBreak: 'break-all',
              userSelect: 'all',
            }}
          >
            {token}
          </div>
          <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
            <Btn kind="primary" onClick={copy}>
              {copied ? 'Copied!' : 'Copy token'}
            </Btn>
            <Btn kind="secondary" onClick={onClose}>
              Close
            </Btn>
          </div>
        </div>
      </Card>
    </div>
  );
}

// ── Add-token side panel ──────────────────────────────────────────────────────

const EXPIRY_PRESETS = [
  { label: '7 days', days: 7 },
  { label: '30 days', days: 30 },
  { label: '90 days', days: 90 },
  { label: 'Never', days: 0 },
] as const;

function AddTokenPanel({
  onCancel,
  onCreated,
  availableRoles,
}: {
  onCancel: () => void;
  onCreated: (plaintext: string) => void;
  availableRoles: Role[];
}) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [role, setRole] = useState<string>(availableRoles[0] ?? 'viewer');
  const [expiryPreset, setExpiryPreset] = useState<number>(30);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      let expiresAt: string | undefined;
      if (expiryPreset > 0) {
        const d = new Date();
        d.setDate(d.getDate() + expiryPreset);
        expiresAt = d.toISOString();
      }
      const res = await tokensApi.create({ name, role, expiresAt });
      onCreated(res.token);
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
          background: hexA(t.accent, 0.04),
        }}
      >
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          Create a token
        </span>
      </div>
      <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
        <Label>Name</Label>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="ci-deploy-bot"
          aria-label="Token name"
        />
        <Label>Role</Label>
        <Select
          value={role}
          onChange={(e) => setRole(e.target.value)}
          aria-label="Token role"
        >
          {availableRoles.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </Select>
        <Label>Expiry</Label>
        <Select
          value={String(expiryPreset)}
          onChange={(e) => setExpiryPreset(Number(e.target.value))}
          aria-label="Token expiry"
        >
          {EXPIRY_PRESETS.map((p) => (
            <option key={p.days} value={String(p.days)}>
              {p.label}
            </option>
          ))}
        </Select>
        <div style={{ display: 'flex', gap: 10, marginTop: 22, justifyContent: 'flex-end' }}>
          <Btn kind="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Btn>
          <Btn kind="primary" onClick={submit} disabled={busy || !name || !role}>
            {busy ? 'Creating…' : 'Create token'}
          </Btn>
        </div>
      </div>
    </Card>
  );
}
