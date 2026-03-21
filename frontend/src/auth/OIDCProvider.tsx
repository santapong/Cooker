import { createContext, useContext, useState, useEffect, type ReactNode } from 'react';

interface User {
  sub: string;
  email: string;
  name: string;
  groups: string[];
  roles: string[];
}

interface AuthContextType {
  user: User | null;
  isAuthenticated: boolean;
  login: () => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  isAuthenticated: false,
  login: () => {},
  logout: () => {},
});

export function useAuth() {
  return useContext(AuthContext);
}

export function OIDCProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);

  useEffect(() => {
    // In dev mode (OIDC disabled), use a default user
    // In production, initialize oidc-client-ts UserManager
    setUser({
      sub: 'dev-user',
      email: 'dev@cooker.local',
      name: 'Developer',
      groups: ['admin'],
      roles: ['admin'],
    });
  }, []);

  const login = () => {
    // In production: userManager.signinRedirect()
    console.log('OIDC login redirect');
  };

  const logout = () => {
    // In production: userManager.signoutRedirect()
    setUser(null);
  };

  return (
    <AuthContext.Provider value={{ user, isAuthenticated: !!user, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}
