import type { ComponentProps } from 'react';
import type { NativeRuntimeSubject } from '../../types/nativeRuntime';
import { nativeSubjectKey } from '../../utils/nativeRuntime';
import { useNativeStageLogs } from '../../hooks/useNativeStageLogs';
import TelemetryConsole from './TelemetryConsole';

type Props = ComponentProps<typeof TelemetryConsole>;

export default function RunTelemetryConsole({ nativeSubject, ...props }: Props & { nativeSubject?: NativeRuntimeSubject }) {
  return nativeSubject ? <NativeConsole key={nativeSubjectKey(nativeSubject)} subject={nativeSubject} consoleProps={props} /> : <TelemetryConsole {...props} />;
}

function NativeConsole({ subject, consoleProps }: { subject: NativeRuntimeSubject; consoleProps: Props }) {
  const logs = useNativeStageLogs(subject);
  return <TelemetryConsole {...consoleProps} lines={logs.lines} live={logs.connected} banner={logs.truncated ? 'Some live output was dropped. Capture a snapshot to inspect retained Docker logs.' : consoleProps.banner} />;
}
