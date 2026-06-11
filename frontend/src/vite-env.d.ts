/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_OIDC_ENABLED?: string;
  readonly VITE_OIDC_AUTHORITY?: string;
  readonly VITE_OIDC_CLIENT_ID?: string;
  readonly VITE_OIDC_REDIRECT_URI?: string;
  readonly VITE_OIDC_POST_LOGOUT_REDIRECT_URI?: string;
  readonly VITE_OIDC_SCOPE?: string;
  /** Only for split-origin hosting (e.g. SPA on Vercel, API elsewhere).
   *  Leave unset for the normal single-binary same-origin deployment. */
  readonly VITE_API_BASE_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
