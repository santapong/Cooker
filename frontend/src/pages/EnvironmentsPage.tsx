import { useEffect, useState } from 'react';
import { useEnvironmentStore } from '../stores/environmentStore';
import type { Environment } from '../types/environment';
import { Btn, EmptyState, PageHeader } from '../components/ui/atoms';
import EnvironmentList from '../components/environments/EnvironmentList';
import EnvironmentEditor from '../components/environments/EnvironmentEditor';

export default function EnvironmentsPage() {
  const environments = useEnvironmentStore((s) => s.environments);
  const loading = useEnvironmentStore((s) => s.loading);
  const fetchEnvironments = useEnvironmentStore((s) => s.fetchEnvironments);
  const createEnvironment = useEnvironmentStore((s) => s.createEnvironment);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<Environment | null>(null);

  useEffect(() => {
    fetchEnvironments();
  }, [fetchEnvironments]);

  const seed = async () => {
    setBusy(true);
    try {
      await createEnvironment({
        name: 'dev',
        order: 1,
        target: { type: 'namespace', clusterId: '', namespace: 'cooker-dev', kubeContext: '' },
        promotion: { strategy: 'auto' },
        variables: {},
      });
      await createEnvironment({
        name: 'staging',
        order: 2,
        target: { type: 'namespace', clusterId: '', namespace: 'cooker-staging', kubeContext: '' },
        promotion: { strategy: 'auto' },
        variables: {},
      });
      await createEnvironment({
        name: 'production',
        order: 3,
        target: { type: 'namespace', clusterId: '', namespace: 'cooker-prod', kubeContext: '' },
        promotion: { strategy: 'manual' },
        variables: {},
      });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="promotion path"
        title="Environments"
        subtitle="The conveyor belt your services move along. Deploys land in the leftmost env first, then promote (auto or manual) to the next. Click an env to manage its secrets."
        actions={<Btn kind="primary" icon="plus">Add environment</Btn>}
      />

      {environments.length === 0 && !loading ? (
        // Empty-state CTA — Indie step 2 (W11-A3, PR #50).
        <EmptyState
          title="No environments yet."
          body="Set up dev → staging → production so deploys have a conveyor belt to roll along."
          action={
            <div style={{ display: 'flex', gap: 10, justifyContent: 'center', flexWrap: 'wrap' }}>
              <Btn kind="primary" icon="spark" onClick={seed} disabled={busy}>
                {busy ? 'Setting up…' : 'Seed dev / staging / prod'}
              </Btn>
              <a
                href="https://github.com/santapong/Cooker/blob/main/docs/user-guide/README.md"
                target="_blank"
                rel="noopener noreferrer"
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '8px 16px',
                  border: `1px solid currentColor`,
                  borderRadius: 7,
                  fontSize: 13.5,
                  color: 'inherit',
                  textDecoration: 'none',
                  opacity: 0.7,
                }}
              >
                Read the user guide ↗
              </a>
            </div>
          }
        />
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: selected ? '1fr 420px' : '1fr',
            gap: 22,
          }}
        >
          <EnvironmentList
            environments={environments}
            selected={selected}
            onSelect={(env) => setSelected(selected?.id === env.id ? null : env)}
          />

          {selected && (
            <EnvironmentEditor
              env={selected}
              environments={environments}
              onClose={() => setSelected(null)}
            />
          )}
        </div>
      )}
    </div>
  );
}
