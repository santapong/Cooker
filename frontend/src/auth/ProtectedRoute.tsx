import { type ReactNode } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { useAuth } from './OIDCProvider';

// Design reset (Phase 2): auth-gate logic is plumbing and stays intact;
// the fallback/denied UI is unstyled pending redesign.
interface Props {
  children: ReactNode;
  requiredRoles?: string[];
}

export default function ProtectedRoute({ children, requiredRoles }: Props) {
  const { user, isAuthenticated, isLoading } = useAuth();
  const location = useLocation();

  if (isLoading) {
    return (
      <div style={{ padding: 40 }} aria-busy="true" aria-live="polite">
        Loading…
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
        <div style={{ padding: 40 }}>
          <h2>Access denied</h2>
          <p>You need one of these roles: {requiredRoles.join(', ')}</p>
        </div>
      );
    }
  }

  return <>{children}</>;
}
