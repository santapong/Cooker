import { useEffect } from 'react';
import { useToastStore, type Toast as ToastT } from '../stores/toastStore';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Icon } from './ui/Icon';
import { type Tone } from './ui/atoms';

const AUTO_DISMISS_MS = 6000;

const KIND_TONE: Record<ToastT['kind'], Tone> = {
  info: 'cool',
  success: 'good',
  warning: 'warn',
  error: 'bad',
};

function ToastItem({ toast }: { toast: ToastT }) {
  const t = useTheme();
  const dismiss = useToastStore((s) => s.dismiss);
  const tone = KIND_TONE[toast.kind];
  const colorMap: Record<Tone, string> = {
    good: t.good, bad: t.bad, warn: t.warn, cool: t.cool, accent: t.accent, ember: t.ember, neutral: t.textMute,
  };
  const c = colorMap[tone];

  useEffect(() => {
    if (toast.kind === 'error') return;
    const id = window.setTimeout(() => dismiss(toast.id), AUTO_DISMISS_MS);
    return () => window.clearTimeout(id);
  }, [toast.id, toast.kind, dismiss]);

  return (
    <div
      role="status"
      style={{
        pointerEvents: 'auto',
        display: 'flex',
        alignItems: 'flex-start',
        gap: 12,
        padding: '12px 14px',
        background: t.surface,
        color: t.text,
        border: `1px solid ${hexA(c, 0.45)}`,
        borderLeft: `3px solid ${c}`,
        borderRadius: 8,
        boxShadow: `0 4px 14px ${hexA('#000', 0.18)}`,
        fontFamily: t.sans,
        fontSize: 13,
      }}
    >
      <span style={{ flex: 1, lineHeight: 1.4 }}>{toast.message}</span>
      <button
        type="button"
        onClick={() => dismiss(toast.id)}
        aria-label="dismiss"
        style={{
          background: 'transparent',
          border: 'none',
          color: t.textMute,
          cursor: 'pointer',
          padding: 2,
          display: 'grid',
          placeItems: 'center',
        }}
      >
        <Icon name="close" size={12} />
      </button>
    </div>
  );
}

export function ToastViewport() {
  const toasts = useToastStore((s) => s.toasts);
  if (toasts.length === 0) return null;
  return (
    <div
      aria-live="polite"
      style={{
        pointerEvents: 'none',
        position: 'fixed',
        bottom: 16,
        right: 16,
        zIndex: 9999,
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
        width: 360,
        maxWidth: 'calc(100vw - 32px)',
      }}
    >
      {toasts.map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </div>
  );
}
