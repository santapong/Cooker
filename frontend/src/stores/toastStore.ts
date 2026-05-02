import { create } from 'zustand';

export type ToastKind = 'info' | 'success' | 'warning' | 'error';

export interface Toast {
  id: string;
  kind: ToastKind;
  message: string;
}

interface ToastStore {
  toasts: Toast[];
  push: (t: Omit<Toast, 'id'>) => string;
  dismiss: (id: string) => void;
  clear: () => void;
}

let nextId = 0;
function makeId() {
  nextId += 1;
  return `t${Date.now()}-${nextId}`;
}

export const useToastStore = create<ToastStore>((set) => ({
  toasts: [],
  push: ({ kind, message }) => {
    const id = makeId();
    set((s) => ({ toasts: [...s.toasts, { id, kind, message }] }));
    return id;
  },
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
  clear: () => set({ toasts: [] }),
}));

// Module-level helper so non-React code (the OIDC manager events, the
// API client) can push toasts without going through React context.
export function pushToast(kind: ToastKind, message: string): string {
  return useToastStore.getState().push({ kind, message });
}
