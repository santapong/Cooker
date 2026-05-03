import { type ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { useAuth } from './OIDCProvider';
import { SkeletonStack } from '../components/Skeleton';
import { useTheme } from '../theme/ThemeProvider';

interface Props {
  children: ReactNode;
  requiredRoles?: string[];
}

export default function ProtectedRoute({ children, requiredRoles }: Props) {
  const { user, isAuthenticated, isLoading } = useAuth();
  const t = useTheme();
  const location = useLocation();

  if (isLoading) {
    return (
      <div
        style={{
          padding: 40,
          maxWidth: 640,
          margin: '0 auto',
          display: 'flex',
          flexDirection: 'column',
          gap: 16,
          background: t.bg,
          minHeight: '100vh',
        }}
        aria-busy="true"
        aria-live="polite"
      >
        <SkeletonStack rows={1} height={28} />
        <SkeletonStack rows={4} height={14} />
      </div>
    );
  }

  if (!isAuthenticated) {
    // Pass the current location through so SignInPage can return us
    // here after successful login.
    return <Navigate to="/signin" state={{ from: location.pathname + location.search }} replace />;
  }

  if (requiredRoles && user) {
    const hasRole = requiredRoles.some((role) => user.roles.includes(role));
    if (!hasRole) {
      return (
        <div
          style={{
            minHeight: '100vh',
            background: t.bg,
            color: t.text,
            display: 'grid',
            placeItems: 'center',
            padding: 40,
            textAlign: 'center',
          }}
        >
          <div>
            <h2 style={{ fontFamily: t.serif, fontSize: 28, fontWeight: 500, color: t.text, margin: 0 }}>
              Access denied
            </h2>
            <p style={{ color: t.textSoft, marginTop: 12 }}>
              You need one of these roles: {requiredRoles.join(', ')}
            </p>
          </div>
        </div>
      );
    }
  }

  return <>{children}</>;
}
