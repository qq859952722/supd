// REQ-U-012: 错误边界组件

import { Component, type ErrorInfo, type ReactNode } from 'react'

interface ErrorBoundaryProps {
  children: ReactNode
  fallback?: ReactNode
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

  // P-01-04: 保留原始错误日志记录（console.error），不向用户展示技术堆栈
  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('ErrorBoundary caught error:', error, errorInfo)
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }
      return (
        <div className="flex min-h-[200px] items-center justify-center rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-error)] p-6">
          <div className="text-center">
            <h3 className="text-lg font-semibold text-[var(--color-text-error)]">
              页面出现错误
            </h3>
            <p className="mt-2 text-sm text-[var(--color-text-secondary)]">
              应用发生错误，请刷新页面或联系管理员
            </p>
            {/* P-01-04: 仅开发模式显示原始错误消息，生产环境不暴露技术堆栈 */}
            {import.meta.env.DEV && this.state.error?.message && (
              <p className="mt-2 text-xs text-[var(--color-text-tertiary)] font-mono break-all">
                {this.state.error.message}
              </p>
            )}
            <button
              className="mt-4 rounded-md bg-[var(--color-btn-primary-bg)] px-4 py-2 text-sm text-[var(--color-btn-primary-text)] hover:opacity-90"
              onClick={() => this.setState({ hasError: false, error: null })}
            >
              重试
            </button>
          </div>
        </div>
      )
    }

    return this.props.children
  }
}
