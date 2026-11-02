import { create } from 'zustand';

interface DraftState {
  owner: string | null;
  generation: number;
  drafts: Record<string, unknown>;
  setOwner: (owner: string | null) => void;
  setDraft: (owner: string, key: string, value: unknown) => void;
  clearDraft: (owner: string, key: string) => void;
  clearDraftIf: (owner: string, key: string, expected: unknown, generation: number) => void;
}
/** Session memory only. Never persist configuration/secret drafts to browser storage. */
export const useConfigurationDraftStore = create<DraftState>((set) => ({
  owner: null,
  generation: 0,
  drafts: {},
  setOwner: (owner) => set((state) => state.owner === owner ? state : { owner, generation: state.generation + 1, drafts: {} }),
  setDraft: (owner, key, value) => set((state) => ({ owner, generation: state.owner === owner ? state.generation : state.generation + 1, drafts: { ...(state.owner === owner ? state.drafts : {}), [key]: value } })),
  clearDraftIf: (owner, key, expected, generation) => set((state) => {
    if (state.owner !== owner || state.generation !== generation || state.drafts[key] !== expected) return state;
    const drafts = { ...state.drafts }; delete drafts[key]; return { drafts };
  }),
  clearDraft: (owner, key) => set((state) => {
    if (state.owner !== owner) return state;
    const drafts = { ...state.drafts }; delete drafts[key]; return { drafts };
  }),
}));
