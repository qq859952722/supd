// REQ-U-012: Dialog/Modal对话框组件

import { createContext, useContext, useEffect, useRef, useState, type ReactNode, useCallback } from 'react'
import { cn } from '@/lib/utils'
import { X } from 'lucide-react'

interface DialogContextValue {
  open: boolean
  setOpen: (v: boolean) => void
}

const DialogContext = createContext<DialogContextValue | null>(null)

function useDialog(): DialogContextValue {
  const ctx = useContext(DialogContext)
  if (!ctx) throw new Error('useDialog must be used within Dialog')
  return ctx
}

function Dialog({ children, defaultOpen = false }: { children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <DialogContext.Provider value={{ open, setOpen }}>
      {children}
    </DialogContext.Provider>
  )
}

function DialogTrigger({ children }: { children: ReactNode }) {
  const { setOpen } = useDialog()
  const handleClick = useCallback(() => setOpen(true), [setOpen])

  return (
    <span onClick={handleClick} style={{ cursor: 'pointer' }} role="button" tabIndex={0}>
      {children}
    </span>
  )
}

function DialogOverlay({ className }: { className?: string }) {
  const { setOpen } = useDialog()

  return (
    <div
      className={cn(
        'fixed inset-0 z-50 bg-black/60 backdrop-blur-sm',
        'animate-in fade-in-0',
        className,
      )}
      onClick={() => setOpen(false)}
    />
  )
}

function DialogContent({
  children,
  className,
  showClose = true,
}: {
  children: ReactNode
  className?: string
  showClose?: boolean
}) {
  const { open, setOpen } = useDialog()
  const contentRef = useRef<HTMLDivElement>(null)

  // ESC关闭
  useEffect(() => {
    if (!open) return
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, setOpen])

  // 聚焦管理
  useEffect(() => {
    if (open) contentRef.current?.focus()
  }, [open])

  if (!open) return null

  return (
    <>
      <DialogOverlay />
      <div
        ref={contentRef}
        role="dialog"
        aria-modal="true"
        tabIndex={-1}
        className={cn(
          'fixed left-1/2 top-1/2 z-[51] w-full max-w-lg -translate-x-1/2 -translate-y-1/2',
          'rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-6 shadow-[var(--shadow-lg)]',
          'animate-in zoom-in-95 fade-in-0',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-border-focus)]',
          className,
        )}
      >
        {showClose && (
          <button
            onClick={() => setOpen(false)}
            className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-[var(--color-border-focus)] focus:ring-offset-2"
          >
            <X className="h-4 w-4" />
            <span className="sr-only">关闭</span>
          </button>
        )}
        {children}
      </div>
    </>
  )
}

function DialogHeader({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div className={cn('flex flex-col space-y-1.5 text-center sm:text-left mb-4', className)}>
      {children}
    </div>
  )
}

function DialogTitle({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <h2 className={cn('text-base font-semibold leading-none tracking-tight text-[var(--color-text-primary)]', className)}>
      {children}
    </h2>
  )
}

function DialogDescription({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <p className={cn('text-sm text-[var(--color-text-secondary)]', className)}>
      {children}
    </p>
  )
}

function DialogFooter({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div className={cn('flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2 mt-6', className)}>
      {children}
    </div>
  )
}

export { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter, useDialog }
