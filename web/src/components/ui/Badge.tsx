// REQ-U-012: Badge状态徽标组件
import { type HTMLAttributes, forwardRef } from 'react'
import { cn } from '@/lib/utils'

type BadgeVariant = 'default' | 'success' | 'warning' | 'danger' | 'info' | 'secondary'

interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: BadgeVariant
}

const variantStyles: Record<BadgeVariant, string> = {
  default: 'bg-[var(--color-surface-tertiary)] text-[var(--color-text-primary)]',
  success: 'bg-[var(--color-surface-success)] text-[var(--color-text-success)]',
  warning: 'bg-[var(--color-surface-warning)] text-[var(--color-text-warning)]',
  danger: 'bg-[var(--color-surface-error)] text-[var(--color-text-error)]',
  info: 'bg-[var(--color-surface-info)] text-[var(--color-text-info)]',
  secondary: 'bg-[var(--color-surface-secondary)] text-[var(--color-text-secondary)]',
}

export const Badge = forwardRef<HTMLSpanElement, BadgeProps>(
  ({ className, variant = 'default', children, ...props }, ref) => (
    <span
      ref={ref}
      className={cn(
        'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
        variantStyles[variant],
        className,
      )}
      {...props}
    >
      {children}
    </span>
  ),
)

Badge.displayName = 'Badge'
