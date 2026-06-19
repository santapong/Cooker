import {
  createContext,
  useContext,
  useState,
  useEffect,
  useMemo,
  useCallback,
  type ReactNode,
} from 'react';
import { UserManager, WebStorageStateStore, type User as OidcUser } from 'oidc-client-ts';
import { pushToast } from '../stores/toastStore';
import { authApi, type AuthUser } from '../api/auth';

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
  /** Trigger an OIDC sign-in redirect. No-op if OIDC is disabled. */
  login: () => void;
  /** Sign in with email + password against the local backend. Throws on failure. */
  signinLocal: (email: string, password: string) => Promise<void>;
  /** Sign up with email + password against the local backend. Throws on failure. */
  signupLocal: (email: string, password: string, name?: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  isAuthenticated: false,
  isLoading: true,
  login: () => {},
  signinLocal: async () => {},
  signupLocal: async () => {},
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

const LOCAL_TOKEN_KEY = 'cooker.local.token';

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

// currentAccessToken holds whichever token the API client should
// attach to outgoing requests. Local-auth tokens are stored alongside
// OIDC tokens; whichever is set most recently wins. This keeps the
// API client agnostic of which auth path produced the session.
let currentAccessToken: string | null = null;

export function getAccessToken(): string | null {
  return currentAccessToken;
}

export function triggerSignIn(opts?: { acrValues?: string }): void {
  const manager = getUserManager();
  if (!manager) {
    // No OIDC configured — bounce to the local sign-in page.
    if (window.location.pathname !== '/signin') {
      window.location.assign('/signin');
    }
    return;
  }
  if (opts?.acrValues) {
    void manager.signinRedirect({ acr_values: opts.acrValues });
    return;
  }
  void manager.signinRedirect();
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

function fromAuthUser(u: AuthUser): User {
  return {
    sub: u.id,
    email: u.email,
    name: u.name,
    groups: [],
    roles: u.roles?.length ? u.roles : [u.role],
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

function loadStoredLocalToken(): string | null {
  try {
    return window.localStorage.getItem(LOCAL_TOKEN_KEY);
  } catch {
    return null;
  }
}

function persistLocalToken(token: string | null): void {
  try {
    if (token) {
      window.localStorage.setItem(LOCAL_TOKEN_KEY, token);
    } else {
      window.localStorage.removeItem(LOCAL_TOKEN_KEY);
    }
  } catch {
    // localStorage unavailable (private mode, etc.) — session is in-memory only.
  }
}

export function OIDCProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(oidcEnabled ? null : DEV_USER);
  const [isLoading, setIsLoading] = useState<boolean>(true);

  const manager = useMemo(() => (oidcEnabled ? getUserManager() : null), []);

  // Restore a local-auth session on mount. Runs in parallel with the
  // OIDC restore below; whichever resolves with a user wins.
  useEffect(() => {
    const stored = loadStoredLocalToken();
    if (!stored) {
      if (!oidcEnabled) {
        // Dev mode: no OIDC, no local token — keep DEV_USER and stop loading.
        setIsLoading(false);
      }
      return;
    }
    let cancelled = false;
    currentAccessToken = stored;
    authApi
      .me(stored)
      .then((me) => {
        if (cancelled) return;
        setUser(fromAuthUser(me));
        setIsLoading(false);
      })
      .catch(() => {
        if (cancelled) return;
        // Token expired / invalid — drop it.
        persistLocalToken(null);
        currentAccessToken = null;
        if (!oidcEnabled) {
          setUser(null);
          setIsLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!manager) {
      return;
    }

    let cancelled = false;

    void manager.getUser().then((u) => {
      if (cancelled) return;
      if (u && !u.expired) {
        currentAccessToken = u.access_token;
        setUser(toUser(u));
        setIsLoading(false);
      } else {
        // No valid OIDC session. The local-token effect runs concurrently
        // and will set a user (and call setIsLoading(false)) if the stored
        // token validates. If it fails or there is no stored token, the
        // catch branch in that effect handles the loading state for
        // !oidcEnabled only, leaving us deadlocked when OIDC is enabled
        // but the local token is stale (FE-L6). Resolve loading here so
        // the app can redirect to sign-in rather than hanging on the
        // loading screen. The local-token effect will call setIsLoading(false)
        // again if it resolves successfully — React no-ops on identical state.
        currentAccessToken = null;
        setUser(null);
        setIsLoading(false);
      }
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

    const onSilentRenewError = (err: Error) => {
      pushToast(
        'warning',
        `Session refresh failed (${err.message}). You may be redirected to sign in again.`,
      );
    };

    manager.events.addUserLoaded(onLoaded);
    manager.events.addUserUnloaded(onUnloaded);
    manager.events.addAccessTokenExpired(onExpired);
    manager.events.addSilentRenewError(onSilentRenewError);

    return () => {
      cancelled = true;
      manager.events.removeUserLoaded(onLoaded);
      manager.events.removeUserUnloaded(onUnloaded);
      manager.events.removeAccessTokenExpired(onExpired);
      manager.events.removeSilentRenewError(onSilentRenewError);
    };
  }, [manager]);

  const login = useCallback(() => {
    if (manager) {
      void manager.signinRedirect();
    } else {
      // OIDC disabled — bounce to local sign-in.
      window.location.assign('/signin');
    }
  }, [manager]);

  const signinLocal = useCallback(async (email: string, password: string) => {
    const res = await authApi.signin(email, password);
    persistLocalToken(res.token);
    currentAccessToken = res.token;
    setUser(fromAuthUser(res.user));
  }, []);

  const signupLocal = useCallback(async (email: string, password: string, name?: string) => {
    const res = await authApi.signup(email, password, name);
    persistLocalToken(res.token);
    currentAccessToken = res.token;
    setUser(fromAuthUser(res.user));
  }, []);

  const logout = useCallback(() => {
    persistLocalToken(null);
    currentAccessToken = null;
    if (manager) {
      void manager.signoutRedirect();
    } else {
      setUser(null);
      window.location.assign('/signin');
    }
  }, [manager]);

  return (
    <AuthContext.Provider
      value={{
        user,
        isAuthenticated: !!user,
        isLoading,
        login,
        signinLocal,
        signupLocal,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}
