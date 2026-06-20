/**
 * Unit tests for the licenseStore Zustand store.
 *
 * The api/license module is mocked so the store's state transitions
 * (loading/mutating/error flags, applying the resolved License, and the
 * server-truth reset applied on remove) can be exercised in isolation, with no
 * network or token injection involved.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest';
import type { License } from '../api/license';

const getMock = vi.fn();
const installMock = vi.fn();
const removeMock = vi.fn();

vi.mock('../api/license', () => ({
  licenseApi: {
    get: () => getMock(),
    install: (token: string) => installMock(token),
    remove: () => removeMock(),
  },
}));

import { useLicenseStore } from './licenseStore';

const ACTIVE: License = {
  status: 'active',
  plan: 'crew',
  seats: 0,
  features: ['oidc_mfa', 'managed_secrets'],
  entitlements: { plan: 'crew', seats: 0, features: { oidc_mfa: true, managed_secrets: true } },
  customer: 'ACME Corp',
  issuedAt: '2026-01-01T00:00:00Z',
  expiresAt: '2027-01-01T00:00:00Z',
  installedAt: '2026-06-01T00:00:00Z',
  installedByEmail: 'admin@acme.example',
};

function reset() {
  useLicenseStore.setState({
    license: {
      status: 'none',
      plan: 'free',
      seats: 0,
      features: [],
      entitlements: { plan: 'free', seats: 0, features: {} },
      customer: '',
      issuedAt: '',
      expiresAt: null,
      installedAt: '',
      installedByEmail: '',
    },
    status: 'none',
    loading: false,
    mutating: false,
    error: null,
  });
}

describe('licenseStore', () => {
  beforeEach(() => {
    reset();
    getMock.mockReset();
    installMock.mockReset();
    removeMock.mockReset();
  });

  it('fetch applies the resolved license and entitlements', async () => {
    getMock.mockResolvedValue(ACTIVE);
    await useLicenseStore.getState().fetch();
    const s = useLicenseStore.getState();
    expect(s.status).toBe('active');
    expect(s.license.plan).toBe('crew');
    expect(s.entitlements.features.oidc_mfa).toBe(true);
    expect(s.loading).toBe(false);
    expect(s.error).toBeNull();
  });

  it('fetch records the error message on failure', async () => {
    getMock.mockRejectedValue(new Error('boom'));
    await useLicenseStore.getState().fetch();
    const s = useLicenseStore.getState();
    expect(s.error).toBe('boom');
    expect(s.loading).toBe(false);
  });

  it('install applies the new license and clears mutating', async () => {
    installMock.mockResolvedValue(ACTIVE);
    await useLicenseStore.getState().install('eyJ.token');
    expect(installMock).toHaveBeenCalledWith('eyJ.token');
    const s = useLicenseStore.getState();
    expect(s.status).toBe('active');
    expect(s.mutating).toBe(false);
  });

  it('install rethrows and stores the error on failure', async () => {
    installMock.mockRejectedValue(new Error('invalid token'));
    await expect(useLicenseStore.getState().install('bad')).rejects.toThrow('invalid token');
    const s = useLicenseStore.getState();
    expect(s.error).toBe('invalid token');
    expect(s.mutating).toBe(false);
  });

  it('remove applies the server reset response directly', async () => {
    // seed an active license first
    useLicenseStore.setState({ license: ACTIVE, status: 'active', entitlements: ACTIVE.entitlements });
    // The backend DELETE returns the complete free shape; the store trusts it.
    const FREE: License = {
      status: 'none',
      plan: 'free',
      seats: 0,
      features: [],
      entitlements: { plan: 'free', seats: 0, features: {} },
      customer: '',
      issuedAt: '',
      expiresAt: null,
      installedAt: '',
      installedByEmail: '',
    };
    removeMock.mockResolvedValue(FREE);
    await useLicenseStore.getState().remove();
    const s = useLicenseStore.getState();
    expect(s.status).toBe('none');
    expect(s.license.plan).toBe('free');
    // entitlements reflect the server's free map, not the prior crew one
    expect(s.entitlements.features).toEqual({});
    expect(s.license.customer).toBe('');
    expect(s.mutating).toBe(false);
  });
});
