import { useEffect, useState, type ReactNode } from 'react';

interface Props {
  children: ReactNode;
  confirmLabel?: ReactNode;
  onConfirm: () => void | Promise<void>;
  className?: string;
  disabled?: boolean;
}

/**
 * Two-step destructive action without a browser dialog: the first click
 * arms the button ("Confirm?") for three seconds, the second fires.
 */
export default function ConfirmButton({ children, confirmLabel = 'Confirm?', onConfirm, className, disabled }: Props) {
  const [armed, setArmed] = useState(false);
  useEffect(() => {
    if (!armed) return;
    const t = window.setTimeout(() => setArmed(false), 3000);
    return () => window.clearTimeout(t);
  }, [armed]);
  return (
    <button
      type="button"
      className={armed ? `${className ?? 'btn-danger'} is-armed` : (className ?? 'btn-danger')}
      disabled={disabled}
      aria-live="polite"
      onClick={() => {
        if (!armed) {
          setArmed(true);
          return;
        }
        setArmed(false);
        void onConfirm();
      }}
    >
      {armed ? confirmLabel : children}
    </button>
  );
}
