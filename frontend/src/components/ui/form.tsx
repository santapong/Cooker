import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from 'react';
import Caps from './Caps';

/** Labelled field: small-caps label above the control, optional hint below. */
export function Field({ label, hint, children, className }: { label: ReactNode; hint?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <label className={className ? `field ${className}` : 'field'}>
      <Caps>{label}</Caps>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </label>
  );
}

export function TextInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={props.className ? `input ${props.className}` : 'input'} spellCheck={props.spellCheck ?? false} />;
}

export function TextArea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} className={props.className ? `input ${props.className}` : 'input'} spellCheck={props.spellCheck ?? false} />;
}

export function Select({ options, ...props }: SelectHTMLAttributes<HTMLSelectElement> & { options: { value: string; label: string }[] }) {
  return (
    <select {...props} className={props.className ? `input select ${props.className}` : 'input select'}>
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

export function Check({ label, checked, onChange, disabled }: { label: ReactNode; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <label className="check">
      <input type="checkbox" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span>{label}</span>
    </label>
  );
}

export function Actions({ children }: { children: ReactNode }) {
  return <div className="form-actions">{children}</div>;
}

export function FormError({ children }: { children: ReactNode }) {
  if (!children) return null;
  return (
    <div className="form-error" role="alert">
      {children}
    </div>
  );
}
