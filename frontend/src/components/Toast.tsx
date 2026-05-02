import { useEffect } from 'react';
import { useToastStore, type Toast as ToastT } from '../stores/toastStore';

const AUTO_DISMISS_MS = 6000;

const kindStyles: Record<ToastT['kind'], string> = {
  info: 'border-blue-400 bg-blue-50 text-blue-900',
  success: 'border-green-400 bg-green-50 text-green-900',
  warning: 'border-yellow-400 bg-yellow-50 text-yellow-900',
  error: 'border-red-400 bg-red-50 text-red-900',
};

function ToastItem({ toast }: { toast: ToastT }) {
  const dismiss = useToastStore((s) => s.dismiss);
  useEffect(() => {
    if (toast.kind === 'error') return;
    const t = window.setTimeout(() => dismiss(toast.id), AUTO_DISMISS_MS);
    return () => window.clearTimeout(t);
  }, [toast.id, toast.kind, dismiss]);
  return (
    <div
      role="status"
      className={`pointer-events-auto flex items-start gap-3 rounded border px-4 py-3 shadow-md ${kindStyles[toast.kind]}`}
    >
      <span className="flex-1 text-sm">{toast.message}</span>
      <button
        type="button"
        onClick={() => dismiss(toast.id)}
        aria-label="dismiss"
        className="text-sm opacity-60 hover:opacity-100"
      >
        ×
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
      className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-96 max-w-[calc(100vw-2rem)] flex-col gap-2"
    >
      {toasts.map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </div>
  );
}
