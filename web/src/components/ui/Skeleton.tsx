// REQ-U-012: Skeleton加载占位组件

import { cn } from '@/lib/utils'

export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        'animate-pulse rounded-md bg-[var(--color-surface-tertiary)]',
        className,
      )}
      aria-hidden="true"
    />
  )
}

/** 预设骨架屏组合 */
export function SkeletonCard({ className }: { className?: string }) {
  return (
    <div className={cn('rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-4 space-y-3', className)}>
      <Skeleton className="h-4 w-24" />
      <Skeleton className="h-8 w-full" />
      <div className="flex gap-2">
        <Skeleton className="h-6 flex-1" />
        <Skeleton className="h-6 flex-1" />
      </div>
      <Skeleton className="h-20 w-full" />
    </div>
  )
}

export function SkeletonTable({ rows = 5, cols = 4 }: { rows?: number; cols?: number }) {
  return (
    <div className="w-full space-y-0">
      {/* 表头 */}
      <div className="flex gap-4 border-b border-[var(--color-border-primary)] px-4 py-2">
        {Array.from({ length: cols }).map((_, i) => (
          <Skeleton key={`th-${i}`} className="h-4 flex-1" />
        ))}
      </div>
      {/* 表体 */}
      {Array.from({ length: rows }).map((_, rowIdx) => (
        <div key={`row-${rowIdx}`} className="flex gap-4 border-b border-[var(--color-border-primary)] px-4 py-2">
          {Array.from({ length: cols }).map((_, colIdx) => (
            <Skeleton key={`td-${colIdx}`} className="h-4 flex-1" />
          ))}
        </div>
      ))}
    </div>
  )
}
