import type { AppModel, CanaryConfig } from '../../types/app';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Btn, Card, Input, Label, SectionLabel, Select, Toggle } from '../../components/ui/atoms';

// ServicesPanel groups the app's operational integrations: the GitHub
// webhook (endpoint URL + secret rotation) and the deploy strategy form
// (rolling vs. canary policy, weight, auto-promote). Both are
// action-driven configuration cards, as opposed to OverviewPanel (facts)
// and DeploymentsPanel (deploy activity).
export default function ServicesPanel({
  app,
  webhookUrl,
  showWebhook,
  newSecret,
  rotating,
  onShowWebhook,
  onCancelWebhook,
  onChangeSecret,
  onGenerateSecret,
  onCopyWebhookUrl,
  onRotateWebhook,
  canaryDraft,
  onChangeCanaryDraft,
  savingCanary,
  onSaveCanary,
}: {
  app: AppModel;
  webhookUrl: string;
  showWebhook: boolean;
  newSecret: string;
  rotating: boolean;
  onShowWebhook: () => void;
  onCancelWebhook: () => void;
  onChangeSecret: (value: string) => void;
  onGenerateSecret: () => void;
  onCopyWebhookUrl: () => void;
  onRotateWebhook: () => void;
  canaryDraft: CanaryConfig | null;
  onChangeCanaryDraft: (next: CanaryConfig) => void;
  savingCanary: boolean;
  onSaveCanary: () => void;
}) {
  const t = useTheme();
  return (
    <>
      {/* GitHub webhook card — Indie step 5 (W11-A1, PR #50) */}
      <Card style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <SectionLabel>GitHub webhook</SectionLabel>

        {/* Webhook URL — always shown so operators can copy it before
            setting the secret. The URL is deterministic from the origin
            (backend/internal/server/router.go:191 — no new API field). */}
        <div>
          <Label>Webhook endpoint</Label>
          <Input
            type="text"
            value={webhookUrl}
            readOnly
            aria-label="Webhook endpoint URL"
            style={{ fontFamily: t.mono, fontSize: 11.5 }}
          />
          <Btn
            kind="secondary"
            onClick={onCopyWebhookUrl}
            style={{ marginTop: 8, justifyContent: 'center', width: '100%' }}
          >
            Copy URL
          </Btn>
        </div>

        <div
          style={{
            borderTop: `1px solid ${hexA(t.line, 0.6)}`,
            paddingTop: 12,
            fontSize: 12.5,
            color: t.textSoft,
            lineHeight: 1.5,
          }}
        >
          {app.hasWebhook
            ? 'A webhook secret is set. Rotating it will invalidate any cached secret on the GitHub side.'
            : 'No webhook secret yet. Set one so push events from GitHub trigger deploys.'}
        </div>
        {!showWebhook ? (
          <Btn
            kind="secondary"
            icon="cog"
            onClick={onShowWebhook}
            style={{ justifyContent: 'center' }}
          >
            {app.hasWebhook ? 'Rotate webhook secret' : 'Set webhook secret'}
          </Btn>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <Label>New secret</Label>
            <Input
              type="text"
              value={newSecret}
              onChange={(e) => onChangeSecret(e.target.value)}
              placeholder="paste or generate"
              style={{ fontFamily: t.mono, fontSize: 11.5 }}
            />
            <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
              <Btn kind="ghost" onClick={onGenerateSecret} disabled={rotating}>
                Generate
              </Btn>
              <div style={{ flex: 1 }} />
              <Btn kind="ghost" onClick={onCancelWebhook} disabled={rotating}>
                Cancel
              </Btn>
              <Btn
                kind="primary"
                onClick={onRotateWebhook}
                disabled={rotating || newSecret.length < 8}
              >
                {rotating ? 'Rotating…' : 'Save secret'}
              </Btn>
            </div>
          </div>
        )}
      </Card>

      {/* Deploy strategy card (OR-1). Toggle rolling/canary and tune
          the weight / auto-promote / window, saved via appsApi.update.
          Canary requires a Kubernetes target; we surface a hint when
          the app's target can't run one (the backend returns 422). */}
      {canaryDraft && (
        <Card style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <SectionLabel>Deploy strategy</SectionLabel>
          <div>
            <Label>Strategy</Label>
            <Select
              value={canaryDraft.strategy}
              onChange={(e) =>
                onChangeCanaryDraft({
                  ...canaryDraft,
                  strategy: e.target.value as CanaryConfig['strategy'],
                })
              }
              aria-label="Deploy strategy"
            >
              <option value="rolling">Rolling (replace)</option>
              <option value="canary">Canary (weighted)</option>
            </Select>
          </div>

          {canaryDraft.strategy === 'canary' && (
            <>
              {app.deployTarget.kind !== 'kubernetes' && (
                <div style={{ fontSize: 11.5, color: t.warn, lineHeight: 1.5 }}>
                  Canary needs a Kubernetes deploy target. This app targets{' '}
                  <strong>{app.deployTarget.kind}</strong>; a canary deploy will be rejected.
                </div>
              )}
              <div>
                <Label>Canary weight (%)</Label>
                <Input
                  type="number"
                  min={1}
                  max={99}
                  value={canaryDraft.weight ?? 10}
                  onChange={(e) =>
                    onChangeCanaryDraft({ ...canaryDraft, weight: Number(e.target.value) })
                  }
                  aria-label="Canary weight percent"
                />
              </div>
              <Toggle
                on={!!canaryDraft.autoPromote}
                label="Auto-promote when healthy"
                onClick={() =>
                  onChangeCanaryDraft({ ...canaryDraft, autoPromote: !canaryDraft.autoPromote })
                }
              />
              {canaryDraft.autoPromote && (
                <div>
                  <Label>Health window (seconds)</Label>
                  <Input
                    type="number"
                    min={0}
                    value={canaryDraft.healthWindowSeconds ?? 300}
                    onChange={(e) =>
                      onChangeCanaryDraft({
                        ...canaryDraft,
                        healthWindowSeconds: Number(e.target.value),
                      })
                    }
                    aria-label="Health window seconds"
                  />
                </div>
              )}
            </>
          )}

          <Btn
            kind="primary"
            onClick={onSaveCanary}
            disabled={savingCanary}
            style={{ justifyContent: 'center' }}
          >
            {savingCanary ? 'Saving…' : 'Save strategy'}
          </Btn>
        </Card>
      )}
    </>
  );
}
