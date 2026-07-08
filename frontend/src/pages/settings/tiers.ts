import type { LicensePlan } from '../../api/license';

// ── Pricing tier data (from cosmic-pricing.html) ─────────────────────────────

export interface Tier {
  plan: LicensePlan;
  name: string;
  sub: string;
  price: string;
  unit?: string;
  featured?: boolean;
  /** orb gradient stops, mirroring the mockup's per-tier planet color */
  orb: [string, string];
  glow: string;
  features: string[];
}

export const TIERS: Tier[] = [
  {
    plan: 'free',
    name: 'Explorer',
    sub: 'solo · side projects',
    price: '$0',
    unit: '/ forever',
    orb: ['#7FD0FF', '#3B82C4'],
    glow: 'rgba(91,182,255,0.45)',
    features: [
      'Single-binary, self-hosted',
      'Visual DAG editor + live logs',
      'Unlimited pipelines & runs',
      'Postgres secrets backend',
      'Kubernetes + Fly + Render targets',
      'Community support',
    ],
  },
  {
    plan: 'crew',
    name: 'Crew',
    sub: 'teams · production',
    price: '$49',
    unit: '/ replica / mo',
    featured: true,
    orb: ['#C0A8FF', '#7C5CFF'],
    glow: 'rgba(167,139,250,0.6)',
    features: [
      'Everything in Explorer',
      'Multi-replica HA (Redis-backed)',
      'OIDC + PKCE · 4-role RBAC · MFA',
      'Vault / AWS / GCP secrets',
      'ECS + Cloud Run + SSH targets',
      'Slack / Discord / email notifications',
    ],
  },
  {
    plan: 'constellation',
    name: 'Constellation',
    sub: 'enterprise · multi-tenant',
    price: 'Custom',
    orb: ['#FFE08A', '#E0A93A'],
    glow: 'rgba(255,209,102,0.5)',
    features: [
      'Everything in Crew',
      'SSO group → role mapping',
      'KeepSave multi-tenant secrets',
      'OTLP traces + Prometheus + audit',
      'Air-gapped + cosign verification',
      'Dedicated support & SLA',
    ],
  },
];
