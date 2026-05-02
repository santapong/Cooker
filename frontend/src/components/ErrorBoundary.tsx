import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  children: ReactNode
  /** Optional override for the fallback UI. Receives the error and a reset callback. */
  fallback?: (error: Error, reset: () => void) => ReactNode
}

interface State {
  error: Error | null
}

/**
 * App-root error boundary. Catches uncaught render errors so the
 * whole React tree doesn't crash to a blank page. Production errors
 * are sent to console.error; future revisions should wire to an
 * error-reporting service (Sentry, Bugsnag, etc.).
 *
 * Closes backlog item P5 "Error boundary at the app root".
 */
export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // eslint-disable-next-line no-console
    console.error('ErrorBoundary caught:', error, info.componentStack)
  }

  reset = (): void => {
    this.setState({ error: null })
  }

  render(): ReactNode {
    const { error } = this.state
    if (!error) return this.props.children

    if (this.props.fallback) {
      return this.props.fallback(error, this.reset)
    }

    return (
      <div
        role="alert"
        style={{
          minHeight: '100vh',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: '2rem',
          background: 'var(--color-bg, #0f172a)',
          color: 'var(--color-text, #f1f5f9)',
          fontFamily: 'system-ui, sans-serif',
        }}
      >
        <div style={{ maxWidth: 560 }}>
          <h1 style={{ fontSize: '1.5rem', marginBottom: '0.5rem' }}>Something broke</h1>
          <p style={{ opacity: 0.8, marginBottom: '1rem' }}>
            Cooker hit an unexpected error and couldn't render this page. The
            error has been logged. You can try reloading or going back to the
            home page.
          </p>
          <pre
            style={{
              background: 'rgba(0,0,0,0.3)',
              padding: '0.75rem',
              borderRadius: 6,
              overflowX: 'auto',
              fontSize: '0.8rem',
              marginBottom: '1rem',
            }}
          >
            {error.message}
          </pre>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button
              type="button"
              onClick={this.reset}
              style={{
                padding: '0.5rem 1rem',
                background: 'var(--color-accent, #6366f1)',
                color: '#fff',
                border: 'none',
                borderRadius: 6,
                cursor: 'pointer',
              }}
            >
              Try again
            </button>
            <button
              type="button"
              onClick={() => {
                window.location.assign('/')
              }}
              style={{
                padding: '0.5rem 1rem',
                background: 'transparent',
                color: 'inherit',
                border: '1px solid currentColor',
                borderRadius: 6,
                cursor: 'pointer',
              }}
            >
              Go home
            </button>
          </div>
        </div>
      </div>
    )
  }
}
