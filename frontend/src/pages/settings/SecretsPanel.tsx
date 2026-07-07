import { useEffect, useState } from 'react';
import { settingsApi, type SecretsCheckResult } from '../../api/settings';
import { environmentsApi } from '../../api/environments';
import type { Environment } from '../../types/environment';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Btn, Card, Label, Pill, Select } from '../../components/ui/atoms';
import { useToastStore } from '../../stores/toastStore';
import { SectionHeader } from './SectionHeader';

export default function SecretsPanel() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [envId, setEnvId] = useState('');
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState<SecretsCheckResult | null>(null);

  useEffect(() => {
    environmentsApi
      .list()
      .then(setEnvs)
      .catch(() => setEnvs([]));
  }, []);

  const runTest = async () => {
    setTesting(true);
    setResult(null);
    try {
      setResult(await settingsApi.testSecrets(envId || undefined));
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div style={{ maxWidth: 560 }}>
      <Card pad={0}>
        <SectionHeader title="Secrets backend" count={result ? 1 : 0} />
        <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
          <p style={{ fontFamily: t.sans, fontSize: 13, color: t.textSoft, margin: '0 0 14px' }}>
            Probe the configured secrets backend with a lightweight authenticated read. The check
            reports reachability and latency only — no key names or values are returned.
          </p>
          <Label>Environment to probe (optional)</Label>
          <Select value={envId} onChange={(e) => setEnvId(e.target.value)}>
            <option value="">First environment</option>
            {envs.map((env) => (
              <option key={env.id} value={env.id}>
                {env.name}
              </option>
            ))}
          </Select>
          <div style={{ display: 'flex', gap: 10, marginTop: 18 }}>
            <Btn kind="primary" onClick={runTest} disabled={testing}>
              {testing ? 'Testing…' : 'Test connection'}
            </Btn>
          </div>

          {result && (
            <div
              style={{
                marginTop: 18,
                padding: '12px 14px',
                border: `1px solid ${t.line}`,
                borderRadius: 8,
                background: hexA(result.ok ? '#2e7d32' : '#c62828', 0.05),
                display: 'flex',
                flexDirection: 'column',
                gap: 6,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                <Pill tone={result.ok ? 'good' : 'bad'}>{result.ok ? 'reachable' : 'failed'}</Pill>
                <span style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>
                  {result.backend}
                </span>
                <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute }}>
                  {result.latencyMs} ms
                </span>
              </div>
              {result.error && (
                <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                  {result.error}
                </span>
              )}
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}
