import { describe, expect, it, vi } from 'vitest';
vi.mock('./client', () => ({ get: vi.fn(), post: vi.fn() }));
import { post } from './client';
import { isNativeRuleFor, nativeAutomationApi, nativePreviewUsable, type NativeTriggerPreview } from './nativeAutomation';

const preview: NativeTriggerPreview = {
  rule: { id: 'rule1', version: 3, name: 'Release', enabled: true, appId: '', targetKind: 'pipeline', targetId: 'pipeline1', pipelineVersion: 8, source: { connectionId: 'source1' }, refKind: 'tag', match: 'exact', include: ['v1'], exclude: [], approval: 'review', planHash: 'a'.repeat(64), approvedEnvelopeHash: 'b'.repeat(64) },
  envelope: { hash: 'b'.repeat(64), commit: 'c'.repeat(40), files: ['Dockerfile', '.dockerignore'] },
};
describe('native rule review contract', () => {
  it('submits the native target and exact reviewed hashes without a Compose graph', async () => {
    await nativeAutomationApi.preview(preview.rule, 'refs/tags/v1');
    expect(post).toHaveBeenLastCalledWith('/admin/trigger-rules/preview', { rule: preview.rule, ref: 'refs/tags/v1' });
    await nativeAutomationApi.save(preview, 'refs/tags/v1');
    expect(post).toHaveBeenLastCalledWith('/admin/trigger-rules', { rule: preview.rule, ref: 'refs/tags/v1', expectedEnvelopeHash: preview.envelope.hash, expectedPlanHash: preview.rule.planHash });
    expect(preview.envelope).not.toHaveProperty('graph');
  });
  it('allows only a reviewed version of this exact native target', () => {
    expect(nativePreviewUsable(preview, 'pipeline1', 8)).toBe(true);
    expect(nativePreviewUsable(preview, 'another-pipeline', 8)).toBe(false);
    expect(nativePreviewUsable(preview, 'pipeline1', 9)).toBe(false);
    expect(nativePreviewUsable(preview, 'pipeline1')).toBe(false);
    expect(isNativeRuleFor({ ...preview.rule, appId: 'contradictory-app' }, 'pipeline1')).toBe(false);
    expect(nativePreviewUsable({ ...preview, rule: { ...preview.rule, approvedEnvelopeHash: 'd'.repeat(64) } }, 'pipeline1', 8)).toBe(false);
    expect(nativePreviewUsable({ ...preview, rule: { ...preview.rule, planHash: undefined } }, 'pipeline1', 8)).toBe(false);
  });
  it.each([null, undefined, {}, { rule: preview.rule }, { envelope: preview.envelope }, { rule: preview.rule, envelope: null }, { ...preview, envelope: { ...preview.envelope, files: [null] } }, { ...preview, rule: { ...preview.rule, include: null } }])('keeps malformed review responses unavailable without throwing: %j', (invalid) => {
    expect(nativePreviewUsable(invalid, 'pipeline1', 8)).toBe(false);
  });
  it('encodes rule IDs for actions and uses typed refs for missed-change evaluation', async () => {
    await nativeAutomationApi.check('rule/unsafe?value', 'refs/heads/release/next');
    expect(post).toHaveBeenLastCalledWith('/admin/trigger-rules/rule%2Funsafe%3Fvalue/check', { ref: 'refs/heads/release/next' });
    await nativeAutomationApi.disable('rule/unsafe?value');
    expect(post).toHaveBeenLastCalledWith('/admin/trigger-rules/rule%2Funsafe%3Fvalue/disable');
  });
});
