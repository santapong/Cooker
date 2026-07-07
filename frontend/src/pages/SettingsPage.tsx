import { useState } from 'react';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { PageHeader } from '../components/ui/atoms';
import RegistriesPanel from './settings/RegistriesPanel';
import ClustersPanel from './settings/ClustersPanel';
import SecretsPanel from './settings/SecretsPanel';
import TokensPanel from './settings/TokensPanel';
import LicensePanel from './settings/LicensePanel';

type Tab = 'registries' | 'clusters' | 'secrets' | 'tokens' | 'license';

export default function SettingsPage() {
  const t = useTheme();
  const [tab, setTab] = useState<Tab>('registries');

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="cluster + registry config"
        title="Settings"
        subtitle="Things Cooker needs to talk to: image registries it should authenticate against, Kubernetes contexts it can deploy into."
      />

      <div
        style={{
          display: 'flex',
          padding: 3,
          background: t.surfaceAlt,
          border: `1px solid ${t.line}`,
          borderRadius: 8,
          width: 'fit-content',
          marginBottom: 22,
        }}
      >
        {(['registries', 'clusters', 'secrets', 'tokens', 'license'] as const).map((id) => (
          <button
            key={id}
            onClick={() => setTab(id)}
            style={{
              background: tab === id ? t.surface : 'transparent',
              color: tab === id ? t.text : t.textMute,
              fontFamily: t.sans,
              fontSize: 12.5,
              fontWeight: 600,
              letterSpacing: 0.4,
              textTransform: 'uppercase',
              border: 'none',
              padding: '6px 16px',
              borderRadius: 5,
              cursor: 'pointer',
              boxShadow: tab === id ? `0 1px 2px ${hexA('#000', 0.06)}` : 'none',
            }}
          >
            {id}
          </button>
        ))}
      </div>

      {tab === 'registries' ? (
        <RegistriesPanel />
      ) : tab === 'clusters' ? (
        <ClustersPanel />
      ) : tab === 'secrets' ? (
        <SecretsPanel />
      ) : tab === 'tokens' ? (
        <TokensPanel />
      ) : (
        <LicensePanel />
      )}
    </div>
  );
}
