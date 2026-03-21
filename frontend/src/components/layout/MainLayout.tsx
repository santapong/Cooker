import { ReactNode } from 'react';
import Sidebar from './Sidebar';
import TopBar from './TopBar';

export default function MainLayout({ children }: { children: ReactNode }) {
  return (
    <div style={styles.layout}>
      <Sidebar />
      <div style={styles.main}>
        <TopBar />
        <div style={styles.content}>{children}</div>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  layout: { display: 'flex', minHeight: '100vh', width: '100%' },
  main: { flex: 1, display: 'flex', flexDirection: 'column' },
  content: { flex: 1, padding: 20, overflow: 'auto' },
};
