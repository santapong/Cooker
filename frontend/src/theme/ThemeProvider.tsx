import { createContext, useContext, useMemo, type ReactNode } from 'react';
import { cookerTheme, type CookerTheme } from './tokens';
import { useUIStore } from '../stores/uiStore';

const ThemeContext = createContext<CookerTheme>(cookerTheme('dark'));

export function ThemeProvider({ children }: { children: ReactNode }) {
  const themeMode = useUIStore((s) => s.themeMode);
  const t = useMemo(() => cookerTheme(themeMode), [themeMode]);
  return <ThemeContext.Provider value={t}>{children}</ThemeContext.Provider>;
}

export function useTheme(): CookerTheme {
  return useContext(ThemeContext);
}
