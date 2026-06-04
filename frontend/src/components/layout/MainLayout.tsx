import type { ReactNode } from 'react';
import Sidebar from './Sidebar';
import TopBar from './TopBar';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Starfield } from '../ui/Starfield';

interface Props {
  children: ReactNode;
  fullBleed?: boolean;
}

export default function MainLayout({ children, fullBleed = false }: Props) {
  const t = useTheme();
  const dark = t.mode === 'dark';
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
        {/* content area: nebula-bloom + canvas-gradient backdrop; the starfield
            sits behind the scrolling content. fullBleed pages (the pipeline
            editor) own their own canvas backdrop, so we skip the starfield. */}
        <main
          style={{
            flex: 1,
            minHeight: 0,
            position: 'relative',
            overflow: 'hidden',
            background: `radial-gradient(120% 90% at 85% -10%, ${hexA(t.violetGlow, dark ? 0.16 : 0.08)} 0%, transparent 50%),
                         radial-gradient(110% 90% at 0% 110%, ${hexA(t.cyan, dark ? 0.1 : 0.06)} 0%, transparent 50%),
                         linear-gradient(${t.canvasTop}, ${t.canvasBot})`,
          }}
        >
          {!fullBleed && <Starfield seed={17} density={dark ? 64 : 16} nebula={false} />}
          <div
            style={{
              position: 'relative',
              height: '100%',
              overflow: fullBleed ? 'hidden' : 'auto',
            }}
          >
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
