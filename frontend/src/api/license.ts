import { get, post, del } from './client';

/** License lifecycle status, mirroring the backend contract. */
export type LicenseStatus = 'active' | 'expired' | 'none';

/** Commercial plan tier resolved from the license (or 'free' when none). */
export type LicensePlan = 'free' | 'crew' | 'constellation';

/**
 * Entitlements is the resolved feature map for the current plan. `features`
 * is a boolean map keyed by entitlement name (e.g. `oidc_mfa`,
 * `managed_secrets`, `sso_group_map`, `audit_otlp`).
 */
export interface Entitlements {
  plan: LicensePlan;
  seats: number;
  features: Record<string, boolean>;
}

/**
 * License is the response shape for every /license endpoint. The raw signed
 * token is NEVER returned by the backend; only the resolved claims are.
 *
 * When no license is installed the backend returns `status: 'none'`,
 * `plan: 'free'` and empty entitlements.
 */
export interface License {
  status: LicenseStatus;
  plan: LicensePlan;
  seats: number;
  features: string[];
  entitlements: Entitlements;
  customer: string;
  issuedAt: string;
  /** null when the license never expires (or none installed). */
  expiresAt: string | null;
  installedAt: string;
  installedByEmail: string;
}

export const licenseApi = {
  /** GET /license — any authenticated user. */
  get: () => get<License>('/license'),

  /** POST /license — admin only. Body carries the signed token. */
  install: (token: string) => post<License>('/license', { token }),

  /** DELETE /license — admin only. Returns the reset (free) shape. */
  remove: () => del<License>('/license'),
};
