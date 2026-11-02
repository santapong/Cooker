import { describe, it, expect } from 'vitest';
import { appHealth, appURL, runState } from './workspace';

describe('workspace state is observed, not inferred', () => {
  it('does not report health when it has never been checked', () => {
    expect(appHealth({}).label).toBe('Health not checked');
    expect(appHealth({ healthStatus: 'unknown' }).variant).toBe('muted');
    expect(appHealth({ healthStatus: 'failed' }).variant).toBe('fail');
  });
  it('distinguishes queued work and approval pauses from success', () => {
    expect(runState('pending').label).toBe('Queued');
    expect(runState('needs_attention').label).toBe('Needs attention');
    expect(runState('success').label).toBe('Succeeded');
  });
});

describe('application links', () => {
  it('permits public HTTPS and explicitly configured local HTTP', () => {
    expect(appURL('https://example.com/app')).toBe('https://example.com/app');
    expect(appURL('http://127.0.0.1:5678')).toBe('http://127.0.0.1:5678/');
  });
  it('rejects executable, relative, invalid, and credential-bearing addresses', () => {
    for (const value of ['javascript:alert(1)', 'data:text/html,test', '/app', 'invalid', 'https://user:password@example.com', undefined]) expect(appURL(value)).toBeUndefined();
  });
});
