import type { ReactNode } from 'react';
import Sidebar from './Sidebar';
import TopBar from './TopBar';
import { useTheme } from '../../theme/ThemeProvider';

interface Props {
  children: ReactNode;
  fullBleed?: boolean;
}

export default function MainLayout({ children, fullBleed = false }: Props) {
  const t = useTheme();
  return (
    <div
      style={{
        display: 'flex',
        height: '100vh',
        width: '100%',
        background: t.bg,
        color: t.text,
        fontFamily: t.sans,
        overflow: 'hidden',
      }}
    >
      <Sidebar />
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        <TopBar />
        <main
          style={{
            flex: 1,
            overflow: fullBleed ? 'hidden' : 'auto',
            position: 'relative',
            background: t.bg,
          }}
        >
          {children}
        </main>
      </div>
    </div>
  );
}
