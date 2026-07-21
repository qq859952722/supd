// REQ-U-012: Input输入框组件
import { forwardRef, type InputHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, type = 'text', ...props }, ref) => (
    <input
      type={type}
      className={cn(
        'flex h-9 w-full rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] px-3 py-1.5',
        'text-sm text-[var(--color-text-primary)] placeholder:text-[var(--color-text-tertiary)]',
        'transition-colors duration-150',
        'focus:border-[var(--color-border-focus)] focus:outline-none focus:ring-1 focus:ring-[var(--color-border-focus)]',
        'disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      ref={ref}
      {...props}
    />
  ),
)

Input.displayName = 'Input'
