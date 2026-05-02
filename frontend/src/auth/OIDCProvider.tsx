import {
  createContext,
  useContext,
  useState,
  useEffect,
  useMemo,
  type ReactNode,
} from 'react';
import { UserManager, WebStorageStateStore, type User as OidcUser } from 'oidc-client-ts';

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
  isLoading: boolean;
  login: () => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  isAuthenticated: false,
  isLoading: true,
  login: () => {},
  logout: () => {},
});

export function useAuth() {
  return useContext(AuthContext);
}

const oidcEnabled = import.meta.env.VITE_OIDC_ENABLED === 'true';

const DEV_USER: User = {
  sub: 'dev-user',
  email: 'dev@cooker.local',
  name: 'Developer',
  groups: ['cooker-admins'],
  roles: ['admin'],
};

let cachedManager: UserManager | null = null;

export function getUserManager(): UserManager | null {
  if (!oidcEnabled) return null;
  if (cachedManager) return cachedManager;

  const authority = import.meta.env.VITE_OIDC_AUTHORITY;
  const clientId = import.meta.env.VITE_OIDC_CLIENT_ID;
  const redirectUri =
    import.meta.env.VITE_OIDC_REDIRECT_URI ?? `${window.location.origin}/callback`;
  const postLogoutRedirectUri =
    import.meta.env.VITE_OIDC_POST_LOGOUT_REDIRECT_URI ?? window.location.origin;
  const scope = import.meta.env.VITE_OIDC_SCOPE ?? 'openid profile email groups';

  if (!authority || !clientId) {
    throw new Error(
      'OIDC enabled but VITE_OIDC_AUTHORITY or VITE_OIDC_CLIENT_ID is missing',
    );
  }

  cachedManager = new UserManager({
    authority,
    client_id: clientId,
    redirect_uri: redirectUri,
    post_logout_redirect_uri: postLogoutRedirectUri,
    response_type: 'code',
    scope,
    userStore: new WebStorageStateStore({ store: window.localStorage }),
    automaticSilentRenew: true,
  });

  return cachedManager;
}

let currentAccessToken: string | null = null;

export function getAccessToken(): string | null {
  return currentAccessToken;
}

export function triggerSignIn(): void {
  const manager = getUserManager();
  if (manager) {
    void manager.signinRedirect();
  }
}

function toUser(oidcUser: OidcUser): User {
  const profile = oidcUser.profile as {
    sub?: string;
    email?: string;
    name?: string;
    groups?: string[];
    roles?: string[];
  };
  const groups = profile.groups ?? [];
  const roles = profile.roles ?? mapGroupsToRoles(groups);
  return {
    sub: profile.sub ?? '',
    email: profile.email ?? '',
    name: profile.name ?? '',
    groups,
    roles,
  };
}

function mapGroupsToRoles(groups: string[]): string[] {
  const map: Record<string, string> = {
    'cooker-admins': 'admin',
    'cooker-operators': 'operator',
    'cooker-approvers': 'approver',
    'cooker-viewers': 'viewer',
  };
  const roles = new Set<string>();
  for (const g of groups) {
    if (map[g]) roles.add(map[g]);
  }
  if (roles.size === 0) roles.add('viewer');
  return Array.from(roles);
}

export function OIDCProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(oidcEnabled ? null : DEV_USER);
  const [isLoading, setIsLoading] = useState<boolean>(oidcEnabled);

  const manager = useMemo(() => (oidcEnabled ? getUserManager() : null), []);

  useEffect(() => {
    if (!manager) {
      currentAccessToken = null;
      return;
    }

    let cancelled = false;

    void manager.getUser().then((u) => {
      if (cancelled) return;
      if (u && !u.expired) {
        currentAccessToken = u.access_token;
        setUser(toUser(u));
      } else {
        currentAccessToken = null;
        setUser(null);
      }
      setIsLoading(false);
    });

    const onLoaded = (u: OidcUser) => {
      currentAccessToken = u.access_token;
      setUser(toUser(u));
    };
    const onUnloaded = () => {
      currentAccessToken = null;
      setUser(null);
    };
    const onExpired = () => {
      currentAccessToken = null;
      setUser(null);
    };

    manager.events.addUserLoaded(onLoaded);
    manager.events.addUserUnloaded(onUnloaded);
    manager.events.addAccessTokenExpired(onExpired);

    return () => {
      cancelled = true;
      manager.events.removeUserLoaded(onLoaded);
      manager.events.removeUserUnloaded(onUnloaded);
      manager.events.removeAccessTokenExpired(onExpired);
    };
  }, [manager]);

  const login = () => {
    if (manager) {
      void manager.signinRedirect();
    }
  };

  const logout = () => {
    if (manager) {
      void manager.signoutRedirect();
    } else {
      setUser(null);
    }
  };

  return (
    <AuthContext.Provider
      value={{ user, isAuthenticated: !!user, isLoading, login, logout }}
    >
      {children}
    </AuthContext.Provider>
  );
}
