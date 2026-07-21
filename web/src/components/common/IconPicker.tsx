// 可视化图标选择器 — 按类别分组展示图标，支持搜索
import { useState, useRef, useEffect, useMemo } from 'react'
import { ChevronDown, Search } from 'lucide-react'
import { IconRenderer, AVAILABLE_ICONS } from './IconRenderer'
import { cn } from '@/lib/utils'

interface IconPickerProps {
  value: string
  onChange: (value: string) => void
  className?: string
}

export function IconPicker({ value, onChange, className }: IconPickerProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const wrapperRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // 点击外部关闭
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setOpen(false)
        setSearch('')
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  // 打开时自动聚焦搜索框
  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [open])

  // 按类别分组（可被搜索过滤）
  const grouped = useMemo(() => {
    const filtered = search.trim()
      ? AVAILABLE_ICONS.filter((ic) =>
          ic.value.toLowerCase().includes(search.toLowerCase()) ||
          ic.category.toLowerCase().includes(search.toLowerCase()),
        )
      : AVAILABLE_ICONS
    const groups: Record<string, typeof AVAILABLE_ICONS> = {}
    for (const ic of filtered) {
      if (!groups[ic.category]) groups[ic.category] = []
      groups[ic.category]!.push(ic)
    }
    return groups
  }, [search])

  return (
    <div ref={wrapperRef} className={cn('relative', className)}>
      {/* 触发按钮：显示当前选中的图标 + 名称 */}
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className={cn(
          'flex h-9 w-full items-center justify-between rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] px-3 py-1.5',
          'text-sm text-[var(--color-text-primary)]',
          'transition-colors duration-150',
          'focus:border-[var(--color-border-focus)] focus:outline-none focus:ring-1 focus:ring-[var(--color-border-focus)]',
          'hover:border-[var(--color-border-primary)]',
          open && 'border-[var(--color-border-focus)] ring-1 ring-[var(--color-border-focus)]',
        )}
      >
        <span className="flex items-center gap-2">
          <IconRenderer
            name={value}
            className="h-4 w-4 text-[var(--color-brand-primary)]"
          />
          <span className={cn(!value && 'text-[var(--color-text-tertiary)]')}>
            {value || '选择图标'}
          </span>
        </span>
        <ChevronDown
          className={cn(
            'h-4 w-4 shrink-0 text-[var(--color-text-tertiary)] transition-transform',
            open && 'rotate-180',
          )}
        />
      </button>

      {/* 下拉面板：图标网格 + 搜索 */}
      {open && (
        <div
          className={cn(
            'absolute z-50 mt-1 w-full rounded-md border border-[var(--color-border-secondary)]',
            'bg-[var(--color-surface-secondary)] shadow-lg',
          )}
        >
          {/* 搜索框 */}
          <div className="border-b border-[var(--color-border-secondary)] p-2">
            <div className="relative">
              <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-[var(--color-text-tertiary)]" />
              <input
                ref={inputRef}
                type="text"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="搜索图标..."
                className={cn(
                  'w-full rounded border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] py-1 pl-7 pr-2',
                  'text-sm text-[var(--color-text-primary)] placeholder:text-[var(--color-text-tertiary)]',
                  'focus:border-[var(--color-border-focus)] focus:outline-none',
                )}
              />
            </div>
          </div>

          {/* 图标网格（按类别分组） */}
          <div className="max-h-72 overflow-y-auto p-2 scrollbar-thin">
            {Object.keys(grouped).length === 0 ? (
              <div className="py-6 text-center text-sm text-[var(--color-text-tertiary)]">
                未找到匹配的图标
              </div>
            ) : (
              Object.entries(grouped).map(([category, icons]) => (
                <div key={category} className="mb-3 last:mb-0">
                  <div className="mb-1.5 text-xs font-medium text-[var(--color-text-tertiary)]">
                    {category}
                  </div>
                  <div className="grid grid-cols-8 gap-1">
                    {icons.map((ic) => (
                      <button
                        key={ic.value}
                        type="button"
                        title={ic.value}
                        onClick={() => {
                          onChange(ic.value)
                          setOpen(false)
                          setSearch('')
                        }}
                        className={cn(
                          'flex h-8 w-8 items-center justify-center rounded transition-colors',
                          ic.value === value
                            ? 'bg-[var(--color-brand-primary)]/20 text-[var(--color-brand-primary)] ring-1 ring-[var(--color-brand-primary)]'
                            : 'text-[var(--color-text-secondary)] hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-text-primary)]',
                        )}
                      >
                        <IconRenderer name={ic.value} className="h-4 w-4" />
                      </button>
                    ))}
                  </div>
                </div>
              ))
            )}
          </div>

          {/* 底部：显示当前选中 */}
          {value && (
            <div className="border-t border-[var(--color-border-secondary)] px-3 py-1.5 text-xs text-[var(--color-text-tertiary)]">
              当前选择：<span className="font-mono text-[var(--color-brand-primary)]">{value}</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
