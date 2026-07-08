import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Btn, Card, Pill } from '../../components/ui/atoms';
import { Icon } from '../../components/ui/Icon';
import { useLicenseStore } from '../../stores/licenseStore';
import { useToastStore } from '../../stores/toastStore';
import { useAuth } from '../../auth/OIDCProvider';
import type { LicensePlan, LicenseStatus } from '../../api/license';
import { TIERS, type Tier } from './tiers';

// ── Helpers ──────────────────────────────────────────────────────────────────

function planLabel(plan: LicensePlan): string {
  switch (plan) {
    case 'crew':
      return 'Crew';
    case 'constellation':
      return 'Constellation';
    default:
      return 'Explorer (Free)';
  }
}

function statusTone(status: LicenseStatus): 'good' | 'warn' | 'neutral' {
  switch (status) {
    case 'active':
      return 'good';
    case 'expired':
      return 'warn';
    default:
      return 'neutral';
  }
}

function fmtDate(iso: string | null): string {
  if (!iso) return '—';
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

// ── Panel ──────────────────────────────────────────────────────────────────

export default function LicensePanel() {
  const { user } = useAuth();
  const isAdmin = (user?.roles ?? []).includes('admin');

  const license = useLicenseStore((s) => s.license);
  const status = useLicenseStore((s) => s.status);
  const loading = useLicenseStore((s) => s.loading);
  const error = useLicenseStore((s) => s.error);
  const fetch = useLicenseStore((s) => s.fetch);

  useEffect(() => {
    fetch();
  }, [fetch]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 26, maxWidth: 1080 }}>
      <CurrentLicense
        loading={loading}
        error={error}
        isAdmin={isAdmin}
        license={license}
        status={status}
        onRetry={fetch}
      />

      {isAdmin && <InstallForm />}

      <TierComparison currentPlan={license.plan} unlocked={license.entitlements.features} />
    </div>
  );
}

// ── Current license summary ──────────────────────────────────────────────────

function CurrentLicense({
  loading,
  error,
  isAdmin,
  license,
  status,
  onRetry,
}: {
  loading: boolean;
  error: string | null;
  isAdmin: boolean;
  license: ReturnType<typeof useLicenseStore.getState>['license'];
  status: LicenseStatus;
  onRetry: () => void;
}) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const remove = useLicenseStore((s) => s.remove);
  const mutating = useLicenseStore((s) => s.mutating);

  const handleRemove = async () => {
    if (!window.confirm('Remove the installed license? The instance will fall back to Explorer (Free) features.')) {
      return;
    }
    try {
      await remove();
      pushToast({ kind: 'success', message: 'License removed. Reverted to Explorer (Free).' });
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
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
          gap: 12,
        }}
      >
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          Current license
        </span>
        <Pill tone={statusTone(status)}>{status}</Pill>
        <div style={{ flex: 1 }} />
        {isAdmin && status !== 'none' && (
          <Btn kind="danger" onClick={handleRemove} disabled={mutating}>
            {mutating ? 'Working…' : 'Remove license'}
          </Btn>
        )}
      </div>

      <div style={{ padding: 18 }}>
        {loading ? (
          <div style={{ color: t.textMute, fontFamily: t.sans, fontSize: 13 }}>Loading license…</div>
        ) : error ? (
          <div
            role="alert"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 12,
              padding: '12px 14px',
              border: `1px solid ${hexA(t.bad, 0.4)}`,
              background: hexA(t.bad, 0.07),
              borderRadius: 8,
            }}
          >
            <span style={{ fontFamily: t.sans, fontSize: 13, color: t.bad, flex: 1 }}>
              Could not load license status: {error}
            </span>
            <Btn kind="secondary" onClick={onRetry}>
              Retry
            </Btn>
          </div>
        ) : (
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))',
              gap: 18,
            }}
          >
            <SummaryField label="Plan" value={planLabel(license.plan)} strong />
            <SummaryField
              label="Customer"
              value={license.customer || (status === 'none' ? 'Unlicensed' : '—')}
            />
            <SummaryField label="Seats" value={license.seats > 0 ? String(license.seats) : 'Unlimited'} />
            <SummaryField label="Expires" value={fmtDate(license.expiresAt)} />
            {status !== 'none' && (
              <SummaryField label="Installed" value={fmtDate(license.installedAt)} />
            )}
            {status !== 'none' && license.installedByEmail && (
              <SummaryField label="Installed by" value={license.installedByEmail} />
            )}
          </div>
        )}

        {!loading && !error && status === 'none' && (
          <p style={{ fontFamily: t.sans, fontSize: 13, color: t.textSoft, margin: '18px 0 0', lineHeight: 1.55 }}>
            No commercial license is installed. This instance runs on the Explorer (Free) tier —
            unlimited pipelines and runs on a single binary.
            {isAdmin
              ? ' Paste a signed license token below to unlock Crew or Constellation features.'
              : ' Ask an administrator to install a license to unlock paid features.'}
          </p>
        )}

        {!loading && !error && status === 'expired' && (
          <p style={{ fontFamily: t.sans, fontSize: 13, color: t.warn, margin: '18px 0 0', lineHeight: 1.55 }}>
            This license has expired. Paid features are locked and the instance has reverted to
            Explorer (Free). Existing pipelines and runs are unaffected.
          </p>
        )}
      </div>
    </Card>
  );
}

function SummaryField({ label, value, strong }: { label: string; value: string; strong?: boolean }) {
  const t = useTheme();
  return (
    <div>
      <div
        style={{
          fontFamily: t.mono,
          fontSize: 10,
          color: t.textMute,
          letterSpacing: 1,
          textTransform: 'uppercase',
          marginBottom: 5,
        }}
      >
        {label}
      </div>
      <div
        style={{
          fontFamily: strong ? t.serif : t.mono,
          fontSize: strong ? 16 : 12.5,
          fontWeight: strong ? 500 : 400,
          color: t.text,
          wordBreak: 'break-word',
        }}
      >
        {value}
      </div>
    </div>
  );
}

// ── Admin install form ───────────────────────────────────────────────────────

function InstallForm() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const install = useLicenseStore((s) => s.install);
  const mutating = useLicenseStore((s) => s.mutating);
  const [token, setToken] = useState('');

  const submit = async () => {
    const trimmed = token.trim();
    if (!trimmed) return;
    try {
      await install(trimmed);
      setToken('');
      pushToast({ kind: 'success', message: 'License installed.' });
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  return (
    <Card pad={0}>
      <div style={{ padding: '14px 18px', borderBottom: `1px solid ${t.line}`, background: hexA(t.accent, 0.04) }}>
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          Install a license
        </span>
      </div>
      <div style={{ padding: 18 }}>
        <p style={{ fontFamily: t.sans, fontSize: 13, color: t.textSoft, margin: '0 0 14px', lineHeight: 1.55 }}>
          Paste the signed license token you received. It is verified offline against an embedded
          public key — Cooker never phones home. The raw token is not stored in the browser.
        </p>
        <label
          htmlFor="license-token"
          style={{
            display: 'block',
            fontFamily: t.mono,
            fontSize: 10.5,
            letterSpacing: 1.2,
            textTransform: 'uppercase',
            color: t.textMute,
            margin: '0 0 6px',
          }}
        >
          License token
        </label>
        <textarea
          id="license-token"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          rows={5}
          placeholder="eyJ…"
          spellCheck={false}
          style={{
            width: '100%',
            padding: '11px 13px',
            background: hexA(t.surfaceSolid, 0.5),
            color: t.text,
            border: `1px solid ${t.line}`,
            borderRadius: 8,
            fontFamily: t.mono,
            fontSize: 12.5,
            lineHeight: 1.5,
            outline: 'none',
            resize: 'vertical',
            wordBreak: 'break-all',
          }}
        />
        <div style={{ display: 'flex', gap: 10, marginTop: 18, justifyContent: 'flex-end' }}>
          <Btn kind="primary" icon="check" onClick={submit} disabled={mutating || !token.trim()}>
            {mutating ? 'Installing…' : 'Install license'}
          </Btn>
        </div>
      </div>
    </Card>
  );
}

// ── Tier comparison cards ────────────────────────────────────────────────────

function TierComparison({
  currentPlan,
  unlocked,
}: {
  currentPlan: LicensePlan;
  unlocked: Record<string, boolean>;
}) {
  const t = useTheme();
  const unlockedKeys = Object.entries(unlocked)
    .filter(([, on]) => on)
    .map(([k]) => k);

  return (
    <div>
      <div style={{ marginBottom: 16 }}>
        <div
          style={{
            fontFamily: t.mono,
            fontSize: 10.5,
            letterSpacing: 1.8,
            textTransform: 'uppercase',
            color: t.violet,
            marginBottom: 8,
          }}
        >
          ✦ pricing
        </div>
        <h2
          style={{
            fontFamily: t.display,
            fontSize: 24,
            fontWeight: 600,
            color: t.text,
            letterSpacing: -0.4,
            margin: 0,
          }}
        >
          Pick your orbit
        </h2>
        <p style={{ fontFamily: t.sans, fontSize: 13.5, color: t.textSoft, margin: '8px 0 0', lineHeight: 1.55 }}>
          Start free on a single binary. Scale to multi-replica HA when your constellation grows.
          Billed per running process — seats are unlimited on every paid tier.
        </p>
      </div>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))',
          gap: 22,
          alignItems: 'start',
        }}
      >
        {TIERS.map((tier) => (
          <TierCard key={tier.plan} tier={tier} current={tier.plan === currentPlan} />
        ))}
      </div>

      {unlockedKeys.length > 0 && (
        <p style={{ fontFamily: t.mono, fontSize: 11, color: t.textMute, margin: '16px 2px 0', lineHeight: 1.6 }}>
          Features unlocked by your current plan:{' '}
          <span style={{ color: t.good }}>{unlockedKeys.join(', ')}</span>
        </p>
      )}
    </div>
  );
}

function TierCard({ tier, current }: { tier: Tier; current: boolean }) {
  const t = useTheme();
  return (
    <Card
      pad={28}
      style={{
        position: 'relative',
        overflow: 'hidden',
        borderColor: current ? hexA(t.violet, 0.6) : tier.featured ? hexA(t.violet, 0.4) : t.line,
        boxShadow: tier.featured
          ? `0 24px 70px ${hexA(t.violet, 0.28)}`
          : `0 10px 30px ${hexA('#000', t.mode === 'dark' ? 0.32 : 0.08)}`,
        background: tier.featured
          ? `radial-gradient(130% 120% at 50% 0%, ${hexA(t.violet, 0.14)}, ${t.surface} 60%)`
          : t.surface,
      }}
    >
      {/* glow top */}
      <div
        style={{
          position: 'absolute',
          right: -30,
          top: -30,
          width: 130,
          height: 130,
          borderRadius: '50%',
          background: `radial-gradient(circle, ${tier.glow}, transparent 70%)`,
          pointerEvents: 'none',
        }}
      />

      {current ? (
        <span style={{ position: 'absolute', top: 18, right: 18 }}>
          <Pill tone="good" solid>
            current
          </Pill>
        </span>
      ) : tier.featured ? (
        <span style={{ position: 'absolute', top: 18, right: 18 }}>
          <Pill tone="accent">most popular</Pill>
        </span>
      ) : null}

      {/* orb */}
      <div
        style={{
          width: 46,
          height: 46,
          borderRadius: '50%',
          marginBottom: 18,
          background: `radial-gradient(circle at 34% 30%, ${tier.orb[0]}, ${tier.orb[1]})`,
          boxShadow: `0 0 26px ${tier.glow}`,
          position: 'relative',
        }}
      />

      <h3 style={{ fontFamily: t.display, fontSize: 22, fontWeight: 600, color: t.text, margin: 0 }}>
        {tier.name}
      </h3>
      <div style={{ fontFamily: t.mono, fontSize: 12, color: t.textMute, marginTop: 6 }}>{tier.sub}</div>

      <div style={{ display: 'flex', alignItems: 'baseline', gap: 6, margin: '22px 0 6px' }}>
        <b style={{ fontFamily: t.display, fontSize: 46, fontWeight: 600, letterSpacing: -1.4, color: t.text }}>
          {tier.price}
        </b>
        {tier.unit && <span style={{ color: t.textMute, fontSize: 14 }}>{tier.unit}</span>}
      </div>

      <ul style={{ listStyle: 'none', padding: 0, margin: '22px 0 26px', display: 'flex', flexDirection: 'column', gap: 12 }}>
        {tier.features.map((f) => (
          <Feature key={f}>{f}</Feature>
        ))}
      </ul>

      <TierCta tier={tier} current={current} />
    </Card>
  );
}

/**
 * Honest tier call-to-action. There is no self-serve checkout in a self-hosted
 * deployment, so we never imply a trial or an in-app purchase flow:
 *  - the customer's current plan renders a disabled "Your plan",
 *  - paid tiers (Crew / Constellation) link a "Contact about <tier>" mailto,
 *  - the free tier renders a non-actionable "Self-hosted default" label.
 */
function TierCta({ tier, current }: { tier: Tier; current: boolean }) {
  const t = useTheme();

  if (current) {
    return (
      <Btn kind="secondary" disabled style={{ width: '100%', justifyContent: 'center' }}>
        Your plan
      </Btn>
    );
  }

  if (tier.plan === 'free') {
    // Free is the self-hosted default — nothing to buy, nothing to start.
    return (
      <Btn kind="secondary" disabled style={{ width: '100%', justifyContent: 'center' }}>
        Self-hosted default
      </Btn>
    );
  }

  // Paid tier: contact the maintainer. mailto is allowed (not a backend URL).
  const subject = encodeURIComponent(`Cooker ${tier.name} license`);
  const primary = tier.featured;
  return (
    <a
      href={`mailto:santapongsondhi@gmail.com?subject=${subject}`}
      className="cc-btn"
      style={{
        width: '100%',
        boxSizing: 'border-box',
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 7,
        padding: '8px 14px',
        borderRadius: 9,
        fontSize: 12.5,
        fontFamily: t.sans,
        fontWeight: 600,
        letterSpacing: 0.1,
        textDecoration: 'none',
        border: '1px solid transparent',
        transition: 'transform .15s, box-shadow .15s, filter .15s',
        whiteSpace: 'nowrap',
        ...(primary
          ? {
              background: `linear-gradient(135deg, ${t.violet}, ${t.violetGlow})`,
              color: '#fff',
              borderColor: hexA(t.violetGlow, 0.6),
              boxShadow: `0 6px 18px ${hexA(t.violetGlow, 0.38)}`,
            }
          : { background: hexA(t.surfaceSolid, 0.6), color: t.text, borderColor: t.line }),
      }}
    >
      Contact about {tier.name}
      <Icon name="arrow" size={14} />
    </a>
  );
}

function Feature({ children }: { children: ReactNode }) {
  const t = useTheme();
  return (
    <li style={{ display: 'flex', gap: 11, fontFamily: t.sans, fontSize: 13.5, color: t.textSoft, alignItems: 'flex-start' }}>
      <span style={{ color: t.good, flexShrink: 0, marginTop: 2, display: 'inline-flex' }}>
        <Icon name="check" size={14} />
      </span>
      <span>{children}</span>
    </li>
  );
}
