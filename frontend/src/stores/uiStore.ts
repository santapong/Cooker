import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { ThemeMode } from '../theme/tokens';

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
