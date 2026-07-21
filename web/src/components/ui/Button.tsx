// REQ-U-012: Button组件 - primary/default/danger三种style
import { forwardRef, type ButtonHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

export type ButtonStyle = 'primary' | 'default' | 'danger'
export type ButtonSize = 'sm' | 'md' | 'lg'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonStyle
  size?: ButtonSize
}

const styleMap: Record<ButtonStyle, string> = {
  primary:
    'bg-[var(--color-btn-primary-bg)] text-[var(--color-btn-primary-text)] hover:bg-[var(--color-btn-primary-hover)] focus-visible:ring-[var(--color-brand-primary)]',
  default:
    'bg-[var(--color-btn-default-bg)] text-[var(--color-btn-default-text)] border border-[var(--color-border-secondary)] hover:bg-[var(--color-btn-default-hover)] hover:border-[var(--color-border-focus)]',
  danger:
    'bg-[var(--color-btn-danger-bg)] text-[var(--color-btn-danger-text)] hover:bg-[var(--color-btn-danger-hover)]',
}

const sizeMap: Record<ButtonSize, string> = {
  sm: 'px-2.5 py-1 text-xs',
  md: 'px-3 py-1.5 text-sm',
  lg: 'px-4 py-2 text-base',
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = 'default', size = 'md', disabled, children, ...props }, ref) => (
    <button
      ref={ref}
      className={cn(
        'inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors duration-150',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--color-bg-primary)]',
        'disabled:pointer-events-none disabled:opacity-50',
        styleMap[variant],
        sizeMap[size],
        className,
      )}
      disabled={disabled}
      {...props}
    >
      {children}
    </button>
  ),
)

Button.displayName = 'Button'
