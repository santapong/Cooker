import { create } from 'zustand';

interface UIStore {
  sidebarCollapsed: boolean;
  activeTab: string;
  toggleSidebar: () => void;
  setActiveTab: (tab: string) => void;
}

export const useUIStore = create<UIStore>((set) => ({
  sidebarCollapsed: false,
  activeTab: 'pipelines',
  toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
  setActiveTab: (tab: string) => set({ activeTab: tab }),
}));
