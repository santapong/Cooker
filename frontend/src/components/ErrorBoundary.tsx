import { Component, type ErrorInfo, type ReactNode } from 'react'

interface Props {
  children: ReactNode
  /** Optional override for the fallback UI. Receives the error and a reset callback. */
  fallback?: (error: Error, reset: () => void) => ReactNode
}

interface State {
  error: Error | null
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
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
          background: '#06061A',
          color: '#ECEBFF',
          fontFamily: '"Inter Tight", "Inter", system-ui, sans-serif',
        }}
      >
        <div style={{ maxWidth: 560 }}>
          <h1
            style={{
              fontFamily: '"Space Grotesk", "Inter Tight", system-ui, sans-serif',
              fontSize: 32,
              fontWeight: 500,
              margin: '0 0 12px',
              letterSpacing: -0.6,
              lineHeight: 1.05,
            }}
          >
            Something broke in orbit.
          </h1>
          <p style={{ opacity: 0.75, marginBottom: 16, lineHeight: 1.5 }}>
            Cooker hit an unexpected error and couldn't render this page. The error has been
            logged. Try reloading or go back home.
          </p>
          <pre
            style={{
              background: 'rgba(0,0,0,0.35)',
              padding: 12,
              borderRadius: 8,
              border: '1px solid rgba(138,126,230,0.32)',
              overflowX: 'auto',
              fontSize: 12,
              fontFamily: '"JetBrains Mono", ui-monospace, monospace',
              color: '#FF6B8A',
              marginBottom: 16,
            }}
          >
            {error.message}
          </pre>
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              type="button"
              onClick={this.reset}
              style={{
                padding: '10px 16px',
                background: 'linear-gradient(135deg, #8B6DFF, #7C5CFF)',
                color: '#fff',
                border: '1px solid rgba(124,92,255,0.6)',
                borderRadius: 8,
                cursor: 'pointer',
                fontFamily: 'inherit',
                fontSize: 13,
                fontWeight: 500,
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
                padding: '10px 16px',
                background: 'transparent',
                color: 'inherit',
                border: '1px solid rgba(138,126,230,0.32)',
                borderRadius: 8,
                cursor: 'pointer',
                fontFamily: 'inherit',
                fontSize: 13,
                fontWeight: 500,
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
