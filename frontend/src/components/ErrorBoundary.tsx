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

    // Design reset (Phase 2): unstyled but functional fallback. Restyle in
    // the redesign; the catch/reset logic above is the part worth keeping.
    return (
      <div role="alert" style={{ padding: '2rem', maxWidth: 560 }}>
        <h1>Something went wrong.</h1>
        <p>Cooker hit an unexpected error and couldn't render this page. The error has been logged.</p>
        <pre style={{ overflowX: 'auto', fontSize: 12 }}>{error.message}</pre>
        <div style={{ display: 'flex', gap: 8 }}>
          <button type="button" onClick={this.reset}>Try again</button>
          <button type="button" onClick={() => { window.location.assign('/') }}>Go home</button>
        </div>
      </div>
    )
  }
}
