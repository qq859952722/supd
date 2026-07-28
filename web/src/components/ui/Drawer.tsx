// 右侧抽屉组件 — 用于运行时参数编辑等非阻塞面板
// 从右侧滑入，不遮挡主内容区，用户可同时操作抽屉与主界面

import { useEffect, useRef, type ReactNode } from 'react'
import { cn } from '@/lib/utils'
import { X } from 'lucide-react'

interface DrawerProps {
  open: boolean
  onOpenChange: (v: boolean) => void
  title: string
  description?: string
  children: ReactNode
  footer?: ReactNode
  width?: number // 默认 460px
}

export function Drawer({ open, onOpenChange, title, description, children, footer, width = 460 }: DrawerProps) {
  const drawerRef = useRef<HTMLDivElement>(null)

  // ESC 关闭
  useEffect(() => {
    if (!open) return
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onOpenChange(false)
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, onOpenChange])

  // 聚焦管理
  useEffect(() => {
    if (open) drawerRef.current?.focus()
  }, [open])

  if (!open) return null

  return (
    <div
      ref={drawerRef}
      role="dialog"
      aria-modal="false"
      tabIndex={-1}
      className={cn(
        'fixed right-0 top-0 bottom-0 z-50 flex flex-col',
        'border-l border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] shadow-[var(--shadow-lg)]',
        'animate-in slide-in-from-right duration-200',
        'focus-visible:outline-none',
      )}
      style={{ width }}
    >
      {/* Header */}
      <div className="flex items-center justify-between border-b border-[var(--color-border-secondary)] px-4 py-3">
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-sm font-semibold text-[var(--color-text-primary)]">{title}</h2>
          {description && (
            <p className="mt-0.5 truncate text-xs text-[var(--color-text-tertiary)]">{description}</p>
          )}
        </div>
        <button
          onClick={() => onOpenChange(false)}
          className="ml-2 rounded-sm opacity-70 transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-[var(--color-border-focus)]"
        >
          <X className="h-4 w-4" />
          <span className="sr-only">关闭</span>
        </button>
      </div>

      {/* Body — 可滚动 */}
      <div className="flex-1 overflow-y-auto px-4 py-3">
        {children}
      </div>

      {/* Footer */}
      {footer && (
        <div className="border-t border-[var(--color-border-secondary)] px-4 py-3">
          {footer}
        </div>
      )}
    </div>
  )
}
