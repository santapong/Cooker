import type { AuthMethods } from '../../api/auth';
import { canSignInWithProvider, SOCIAL_PROVIDERS, type SocialProvider } from '../../auth/socialSignIn';

const names = { github: 'GitHub', google: 'Google' };

function ProviderIcon({ provider }: { provider: SocialProvider }) {
  return provider === 'github' ? (
    <svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M12 .8a11.2 11.2 0 0 0-3.54 21.83c.56.1.77-.24.77-.54v-2.1c-3.13.68-3.79-1.33-3.79-1.33-.51-1.3-1.25-1.65-1.25-1.65-1.02-.7.08-.69.08-.69 1.13.08 1.73 1.16 1.73 1.16 1 .1.86 2.13 3.28 1.51.1-.73.39-1.23.71-1.51-2.5-.29-5.13-1.25-5.13-5.57 0-1.23.44-2.24 1.16-3.03-.12-.29-.5-1.43.11-2.98 0 0 .94-.3 3.08 1.16a10.72 10.72 0 0 1 5.6 0c2.14-1.45 3.08-1.16 3.08-1.16.61 1.55.23 2.69.11 2.98.72.79 1.16 1.8 1.16 3.03 0 4.33-2.64 5.28-5.15 5.56.4.35.76 1.03.76 2.08v3.1c0 .3.2.65.77.54A11.2 11.2 0 0 0 12 .8Z"/></svg>
  ) : (
    <svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M21.6 12.23c0-.71-.07-1.39-.18-2.05H12v3.88h5.39a4.6 4.6 0 0 1-2 3.02v2.5h3.23c1.89-1.74 2.98-4.3 2.98-7.35ZM12 22c2.7 0 4.96-.9 6.62-2.42l-3.23-2.5c-.9.6-2.06.96-3.39.96-2.61 0-4.83-1.76-5.62-4.13H3.04v2.58A10 10 0 0 0 12 22ZM6.38 13.91a6 6 0 0 1 0-3.82V7.51H3.04a10 10 0 0 0 0 8.98l3.34-2.58ZM12 5.96c1.47 0 2.79.5 3.82 1.49l2.87-2.87A9.6 9.6 0 0 0 12 2a10 10 0 0 0-8.96 5.51l3.34 2.58A5.92 5.92 0 0 1 12 5.96Z"/></svg>
  );
}

export default function SocialSignIn({ methods, oidcReady, busy, onSignIn }: {
  methods: AuthMethods; oidcReady: boolean; busy: boolean; onSignIn: (provider: SocialProvider) => void;
}) {
  const unavailable = SOCIAL_PROVIDERS.filter((provider) => !canSignInWithProvider(methods, oidcReady, provider));
  return (
    <div className="auth-social">
      <div className="auth-social-buttons">
        {SOCIAL_PROVIDERS.map((provider) => {
          const available = canSignInWithProvider(methods, oidcReady, provider);
          return (
            <button type="button" key={provider} className="hud-btn hud-btn-secondary auth-social-button"
              disabled={busy || !available} aria-describedby={!available ? 'social-setup-note' : undefined}
              onClick={() => onSignIn(provider)}>
              <ProviderIcon provider={provider} />
              <span>Continue with {names[provider]}</span>
              {!available && <small>Setup needed</small>}
            </button>
          );
        })}
      </div>
      {unavailable.length > 0 && <p id="social-setup-note" className="auth-social-note">{unavailable.map((provider) => names[provider]).join(' and ')} sign-in needs administrator setup.</p>}
    </div>
  );
}
