import { create } from 'zustand';
import { persist } from 'zustand/middleware';

// Theme: the redesign (docs/plans/2026-07-cosmic-frontend-redesign.md) is
// dark-only. `themeMode` keeps its type so the store contract survives, but
// 'light' is deferred and no UI control is wired to setThemeMode.
export type ThemeMode = 'light' | 'dark';

export type UIMode = 'simple' | 'pro';

interface UIStore {
  sidebarCollapsed: boolean;
  activeTab: string;
  mode: UIMode;
  themeMode: ThemeMode;
  /**
   * Calm mode — the WCAG 2.2.2 pause/stop control. When true the shell sets
   * `data-calm="true"` on <html>; CSS swaps every animation for its
   * opacity-only substitute and pauses looping motion (comet, drift,
   * skeleton). JS/WAAPI motion must consult useMotionAllowed().
   */
  calmMode: boolean;
  toggleSidebar: () => void;
  setActiveTab: (tab: string) => void;
  setMode: (mode: UIMode) => void;
  setThemeMode: (mode: ThemeMode) => void;
  setCalmMode: (on: boolean) => void;
  toggleCalmMode: () => void;
}

export const useUIStore = create<UIStore>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      activeTab: 'apps',
      mode: 'pro',
      themeMode: 'dark',
      calmMode: false,
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
      setActiveTab: (tab) => set({ activeTab: tab }),
      setMode: (mode) => set({ mode }),
      setThemeMode: (themeMode) => set({ themeMode }),
      setCalmMode: (calmMode) => set({ calmMode }),
      toggleCalmMode: () => set((state) => ({ calmMode: !state.calmMode })),
    }),
    {
      name: 'cooker-ui',
      partialize: (s) => ({ mode: s.mode, themeMode: s.themeMode, calmMode: s.calmMode }),
    },
  ),
);
