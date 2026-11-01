import { useEffect, useState } from 'react';
import { useToastStore, type Toast as ToastT, type ToastKind } from '../stores/toastStore';
import Caps from './ui/Caps';
import './toast.css';

const AUTO_DISMISS_MS = 6000;
const KIND_LABEL: Record<ToastKind, string> = { info: 'Note', success: 'Done', warning: 'Heads up', error: 'Error' };

function ToastItem({ toast }: { toast: ToastT }) {
  const dismiss = useToastStore((s) => s.dismiss);
  const [held, setHeld] = useState(false);

  // Errors stay until dismissed; everything else leaves after 6 s unless the
  // pointer or keyboard focus is on it (the timer restarts when they leave).
  useEffect(() => {
    if (toast.kind === 'error' || held) return;
    const id = window.setTimeout(() => dismiss(toast.id), AUTO_DISMISS_MS);
    return () => window.clearTimeout(id);
  }, [toast.id, toast.kind, held, dismiss]);

  return (
    <div
      className="toast"
      data-kind={toast.kind}
      role={toast.kind === 'error' ? 'alert' : 'status'}
      onMouseEnter={() => setHeld(true)}
      onMouseLeave={() => setHeld(false)}
      onFocus={() => setHeld(true)}
      onBlur={() => setHeld(false)}
    >
      <div className="toast-body">
        <Caps className="toast-kind">{KIND_LABEL[toast.kind]}</Caps>
        <span className="toast-msg">{toast.message}</span>
      </div>
      <button type="button" className="toast-close" onClick={() => dismiss(toast.id)} aria-label="Dismiss notification">
        ×
      </button>
    </div>
  );
}

export function ToastViewport() {
  const toasts = useToastStore((s) => s.toasts);
  if (toasts.length === 0) return null;
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </div>
  );
}
