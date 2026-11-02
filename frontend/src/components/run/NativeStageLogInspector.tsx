import { useId, useState } from 'react';
import { useAuth } from '../../auth/OIDCProvider';
import type { NativeRuntimeSubject } from '../../types/nativeRuntime';
import { nativeSubjectKey } from '../../utils/nativeRuntime';
import { RuntimeLogBrowser } from '../app/ServiceLogInspector';

export default function NativeStageLogInspector({ subject, containerId }: { subject: NativeRuntimeSubject; containerId: string }) {
  const { user } = useAuth();
  const [open, setOpen] = useState(false);
  const id = useId();
  if (!user?.roles.some((role) => role === 'operator' || role === 'admin')) return <p className="workspace-note">Runtime logs require an operator or administrator role.</p>;
  return <div className="service-log-inspector">
    <button type="button" className="hud-btn" aria-expanded={open} aria-controls={id} onClick={() => setOpen((value) => !value)}>{open ? 'Close log browser' : 'Browse recorded Docker logs'}</button>
    {open && <div id={id}><RuntimeLogBrowser key={nativeSubjectKey(subject)} subject={subject} expectedContainerId={containerId} /></div>}
  </div>;
}
