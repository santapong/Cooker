import { type ReactNode } from 'react';
import { useAuth } from './OIDCProvider';
import { SkeletonStack } from '../components/Skeleton';

interface Props {
  children: ReactNode;
  requiredRoles?: string[];
}

export default function ProtectedRoute({ children, requiredRoles }: Props) {
  const { user, isAuthenticated, isLoading, login } = useAuth();

  if (isLoading) {
    // Skeleton placeholder while auth state restores. Avoids the
    // jarring "Loading..." flash on every refresh. Closes the
    // loading-skeletons portion of backlog item P5.
    return (
      <div
        style={{
          padding: 40,
          maxWidth: 640,
          margin: '0 auto',
          display: 'flex',
          flexDirection: 'column',
          gap: 16,
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
    return (
      <div style={{ padding: 40, textAlign: 'center', color: '#f1f5f9' }}>
        <h2>Authentication Required</h2>
        <p style={{ color: '#94a3b8' }}>Please sign in to access Cooker.</p>
        <button className="btn-primary" onClick={login} style={{ marginTop: 12 }}>
          Sign In with SSO
        </button>
      </div>
    );
  }

  if (requiredRoles && user) {
    const hasRole = requiredRoles.some((role) => user.roles.includes(role));
    if (!hasRole) {
      return (
        <div style={{ padding: 40, textAlign: 'center', color: '#f1f5f9' }}>
          <h2>Access Denied</h2>
          <p style={{ color: '#94a3b8' }}>
            You need one of these roles: {requiredRoles.join(', ')}
          </p>
        </div>
      );
    }
  }

  return <>{children}</>;
}
