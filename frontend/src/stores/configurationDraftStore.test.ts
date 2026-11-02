import { beforeEach, expect, it } from 'vitest';
import { useConfigurationDraftStore } from './configurationDraftStore';
beforeEach(() => useConfigurationDraftStore.getState().setOwner(null));
it('retains same-owner drafts but clears on owner change or sign out', () => {
  const store = useConfigurationDraftStore.getState();
  store.setDraft('owner-one', 'secret', { value: 'synthetic-secret' });
  store.setOwner('owner-one'); expect(useConfigurationDraftStore.getState().drafts.secret).toBeDefined();
  store.setOwner('owner-two'); expect(useConfigurationDraftStore.getState().drafts).toEqual({});
  store.setDraft('owner-two', 'config', 'A=value'); store.setOwner(null); expect(useConfigurationDraftStore.getState().drafts).toEqual({});
});
it('late clear from a departed owner cannot clear another owner draft', () => {
  const store = useConfigurationDraftStore.getState(); store.setDraft('one', 'same', 'one'); store.setDraft('two', 'same', 'two'); store.clearDraft('one', 'same');
  expect(useConfigurationDraftStore.getState().drafts.same).toBe('two');
});
it('uses only memory, without browser-storage persistence hooks', () => {
  expect(useConfigurationDraftStore).not.toHaveProperty('persist');
});
it('conditional completion clears only the sent draft in the same authentication session', () => {
  const store = useConfigurationDraftStore.getState(); const sent = { value: 'sent' }; const newer = { value: 'newer' };
  store.setDraft('one', 'key', sent); const generation = useConfigurationDraftStore.getState().generation; store.setDraft('one', 'key', newer); store.clearDraftIf('one', 'key', sent, generation); expect(useConfigurationDraftStore.getState().drafts.key).toBe(newer);
  store.setOwner(null); store.setDraft('one', 'key', sent); store.clearDraftIf('one', 'key', sent, generation); expect(useConfigurationDraftStore.getState().drafts.key).toBe(sent);
  store.clearDraftIf('one', 'key', sent, useConfigurationDraftStore.getState().generation); expect(useConfigurationDraftStore.getState().drafts).toEqual({});
});
