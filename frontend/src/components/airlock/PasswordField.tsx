import { useId, useState, type InputHTMLAttributes } from 'react';

interface Props extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'id'> {
  label: string;
  hint?: string;
}

export default function PasswordField({ label, hint, className = '', ...props }: Props) {
  const id = useId();
  const [visible, setVisible] = useState(false);
  return (
    <div className="field auth-password-field">
      <label className="caps" htmlFor={id}>{label}</label>
      <div className="auth-password-input">
        <input {...props} id={id} type={visible ? 'text' : 'password'} className={`input ${className}`}
          spellCheck={false} aria-describedby={hint ? `${id}-hint` : undefined} />
        <button type="button" className="auth-password-toggle" aria-controls={id} aria-pressed={visible}
          aria-label={`${visible ? 'Hide' : 'Show'} ${label.toLowerCase()}`} onClick={() => setVisible(!visible)}>
          {visible ? 'Hide' : 'Show'}
        </button>
      </div>
      {hint && <span id={`${id}-hint`} className="field-hint">{hint}</span>}
    </div>
  );
}
