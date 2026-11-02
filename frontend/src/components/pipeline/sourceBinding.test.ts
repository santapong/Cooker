import { describe, expect, it } from 'vitest';
import { sourceBindingIssue, type SourceChoice } from './sourceBinding';

const connections: SourceChoice[] = [{ id: 'git1', name: 'Team Git', provider: 'gitea', enabled: true, version: 3, repositories: ['team/app'] }];
const source = { connectionId: 'git1', repository: 'team/app', fullRef: 'refs/heads/main' };
describe('native source binding guidance', () => {
  it.each(['refs/heads/main', 'refs/heads/release/next', 'refs/tags/v1.0.0'])('accepts exact branch and tag selections: %s', (fullRef) => {
    expect(sourceBindingIssue({ ...source, fullRef }, connections)).toBeNull();
  });
  it.each(['main', 'refs/heads/', 'refs/heads/-bad..ref', 'refs/heads/x.lock', 'refs/heads/a b', 'refs/heads/a@{b', 'refs/tags/*'])('explains ambiguous or unsafe refs: %s', (fullRef) => {
    expect(sourceBindingIssue({ ...source, fullRef }, connections)).toMatch(/exact full ref/);
  });
  it('does not authorize unavailable connections or repositories', () => {
    expect(sourceBindingIssue(source, [])).toMatch(/unavailable/);
    expect(sourceBindingIssue(source, [{ ...connections[0], enabled: false }])).toMatch(/unavailable/);
    expect(sourceBindingIssue({ ...source, repository: 'another/app' }, connections)).toMatch(/allowed/);
  });
  it('leaves unconnected host context without a fabricated Git binding', () => {
    expect(sourceBindingIssue(null, connections)).toBeNull();
  });
});
