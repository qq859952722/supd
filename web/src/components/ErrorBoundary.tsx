// REQ-U-012: 错误边界组件

import { Component, type ErrorInfo, type ReactNode } from 'react'
import { useLocation } from 'react-router'

interface ErrorBoundaryProps {
  children: ReactNode
  fallback?: ReactNode
  resetKey?: string
}

interface ErrorBoundaryState {
  hasError: boolean
  error: Error | null
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('ErrorBoundary caught error:', error, errorInfo)
  }

  componentDidUpdate(prevProps: ErrorBoundaryProps) {
    if (this.state.hasError && prevProps.resetKey !== this.props.resetKey) {
      this.setState({ hasError: false, error: null })
    }
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }
      return (
        <div className="flex min-h-[200px] items-center justify-center rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-error)] p-6">
          <div className="max-w-md text-center">
            <h3 className="text-lg font-semibold text-[var(--color-text-error)]">
              页面出现错误
            </h3>
            <p className="mt-2 text-sm text-[var(--color-text-secondary)]">
              应用发生错误，请重试或刷新页面
            </p>
            {import.meta.env.DEV && this.state.error?.message && (
              <p className="mt-2 break-all font-mono text-xs text-[var(--color-text-tertiary)]">
                {this.state.error.message}
              </p>
            )}
            <div className="mt-4 flex justify-center gap-2">
              <button
                className="rounded-md bg-[var(--color-btn-primary-bg)] px-4 py-2 text-sm text-[var(--color-btn-primary-text)] hover:opacity-90"
                onClick={() => this.setState({ hasError: false, error: null })}
              >
                重试当前页
              </button>
              <button
                className="rounded-md border border-[var(--color-border-primary)] bg-[var(--color-surface-elevated)] px-4 py-2 text-sm text-[var(--color-text-primary)] hover:opacity-90"
                onClick={() => window.location.reload()}
              >
                刷新页面
              </button>
            </div>
          </div>
        </div>
      )
    }

    return this.props.children
  }
}

export function RouteAwareErrorBoundary({ children }: { children: ReactNode }) {
  const location = useLocation()
  return <ErrorBoundary resetKey={location.pathname}>{children}</ErrorBoundary>
}
