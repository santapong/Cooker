import { create } from 'zustand';
import { persist } from 'zustand/middleware';

// Design reset (Phase 2): ThemeMode was owned by the deleted theme/tokens.
// Keep a local type so the store contract survives; the redesign re-owns
// the theme system.
export type ThemeMode = 'light' | 'dark';

export type UIMode = 'simple' | 'pro';

interface UIStore {
  sidebarCollapsed: boolean;
  activeTab: string;
  mode: UIMode;
  themeMode: ThemeMode;
  toggleSidebar: () => void;
  setActiveTab: (tab: string) => void;
  setMode: (mode: UIMode) => void;
  setThemeMode: (mode: ThemeMode) => void;
}

export const useUIStore = create<UIStore>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      activeTab: 'apps',
      mode: 'pro',
      themeMode: 'dark',
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
      setActiveTab: (tab) => set({ activeTab: tab }),
      setMode: (mode) => set({ mode }),
      setThemeMode: (themeMode) => set({ themeMode }),
    }),
    {
      name: 'cooker-ui',
      partialize: (s) => ({ mode: s.mode, themeMode: s.themeMode }),
    },
  ),
);
