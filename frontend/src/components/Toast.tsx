import { useEffect } from 'react';
import { useToastStore, type Toast as ToastT } from '../stores/toastStore';

// Design reset (Phase 2): unstyled but functional. The toast store binding
// is plumbing and stays intact; restyle the markup in the redesign.
const AUTO_DISMISS_MS = 6000;

function ToastItem({ toast }: { toast: ToastT }) {
  const dismiss = useToastStore((s) => s.dismiss);

  useEffect(() => {
    if (toast.kind === 'error') return;
    const id = window.setTimeout(() => dismiss(toast.id), AUTO_DISMISS_MS);
    return () => window.clearTimeout(id);
  }, [toast.id, toast.kind, dismiss]);

  return (
    <div role="status" style={{ pointerEvents: 'auto', display: 'flex', gap: 8, padding: '8px 12px', border: '1px solid #ccc', background: '#fff' }}>
      <span style={{ flex: 1 }}>{toast.message}</span>
      <button type="button" onClick={() => dismiss(toast.id)} aria-label="dismiss">×</button>
    </div>
  );
}

export function ToastViewport() {
  const toasts = useToastStore((s) => s.toasts);
  if (toasts.length === 0) return null;
  return (
    <div
      aria-live="polite"
      style={{ pointerEvents: 'none', position: 'fixed', bottom: 16, right: 16, zIndex: 9999, display: 'flex', flexDirection: 'column', gap: 10, width: 360, maxWidth: 'calc(100vw - 32px)' }}
    >
      {toasts.map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </div>
  );
}
