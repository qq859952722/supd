// REQ-U-012: Select 选择框组件（自定义下拉面板，修复暗色模式对比度问题）
// 原生 <option> 在多数浏览器中无法通过 CSS 控制背景色/文字色，
// 因此用 div 实现自定义下拉面板，确保暗色模式下可读性
import {
  forwardRef,
  useState,
  useRef,
  useEffect,
  useCallback,
  type SelectHTMLAttributes,
  type ButtonHTMLAttributes,
} from 'react'
import { ChevronDown, Check } from 'lucide-react'
import { cn } from '@/lib/utils'

interface SelectOption {
  value: string
  label: string
  disabled?: boolean
}

interface SelectProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, 'children' | 'onChange'> {
  options: SelectOption[]
  placeholder?: string
  // 保持与原生 select 一致的 onChange 签名
  onChange?: (e: { target: { value: string; name?: string } }) => void
}

export const Select = forwardRef<HTMLDivElement, SelectProps>(
  ({ className, options, placeholder = '请选择...', disabled, value, onChange, name, ...props }, ref) => {
    const [open, setOpen] = useState(false)
    const [highlightIdx, setHighlightIdx] = useState(-1)
    const wrapperRef = useRef<HTMLDivElement>(null)
    const buttonRef = useRef<HTMLButtonElement>(null)

    // E-06-001: 移除原生 select 后，外部 ref 改由 wrapper div 承载
    useEffect(() => {
      if (typeof ref === 'function') ref(wrapperRef.current)
      else if (ref) ref.current = wrapperRef.current
    }, [ref])

    // 点击外部关闭
    useEffect(() => {
      if (!open) return
      const handler = (e: MouseEvent) => {
        if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
          setOpen(false)
        }
      }
      document.addEventListener('mousedown', handler)
      return () => document.removeEventListener('mousedown', handler)
    }, [open])

    // 打开时定位到当前选中项
    useEffect(() => {
      if (open) {
        const idx = options.findIndex((o) => o.value === value)
        setHighlightIdx(idx >= 0 ? idx : 0)
      }
    }, [open, options, value])

    const selectOption = useCallback(
      (opt: SelectOption) => {
        if (opt.disabled) return
        onChange?.({ target: { value: opt.value, name } })
        setOpen(false)
        buttonRef.current?.focus()
      },
      [onChange, name],
    )

    const handleKeyDown = (e: React.KeyboardEvent) => {
      if (disabled) return
      switch (e.key) {
        case 'Enter':
        case ' ':
        case 'Spacebar':
          e.preventDefault()
          if (!open) setOpen(true)
          else if (highlightIdx >= 0) {
            const opt = options[highlightIdx]
            if (opt) selectOption(opt)
          }
          break
        case 'Escape':
          if (open) {
            e.preventDefault()
            setOpen(false)
          }
          break
        case 'ArrowDown':
          e.preventDefault()
          if (!open) {
            setOpen(true)
          } else {
            for (let i = highlightIdx + 1; i < options.length; i++) {
              const opt = options[i]
              if (opt && !opt.disabled) {
                setHighlightIdx(i)
                break
              }
            }
          }
          break
        case 'ArrowUp':
          e.preventDefault()
          if (open) {
            for (let i = highlightIdx - 1; i >= 0; i--) {
              const opt = options[i]
              if (opt && !opt.disabled) {
                setHighlightIdx(i)
                break
              }
            }
          }
          break
        case 'Tab':
          if (open) setOpen(false)
          break
      }
    }

    const selectedOption = options.find((o) => o.value === value)
    const displayLabel = selectedOption?.label ?? ''

    return (
      <div ref={wrapperRef} className={cn('relative', disabled && 'opacity-50', className)}>
        {/* 触发按钮：外观与原生 select 一致 */}
        <button
          ref={buttonRef}
          type="button"
          disabled={disabled}
          {...(props as ButtonHTMLAttributes<HTMLButtonElement>)}
          onClick={() => !disabled && setOpen((o) => !o)}
          onKeyDown={handleKeyDown}
          className={cn(
            'flex h-9 w-full items-center justify-between rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] px-3 py-1.5',
            'text-sm text-[var(--color-text-primary)]',
            'transition-colors duration-150',
            'focus:border-[var(--color-border-focus)] focus:outline-none focus:ring-1 focus:ring-[var(--color-border-focus)]',
            'disabled:cursor-not-allowed',
            open && 'border-[var(--color-border-focus)] ring-1 ring-[var(--color-border-focus)]',
          )}
          aria-haspopup="listbox"
          aria-expanded={open}
        >
          <span className={cn('truncate', !displayLabel && 'text-[var(--color-text-tertiary)]')}>
            {displayLabel || placeholder}
          </span>
          <ChevronDown
            className={cn(
              'h-4 w-4 shrink-0 text-[var(--color-text-tertiary)] transition-transform',
              open && 'rotate-180',
            )}
          />
        </button>

        {/* 下拉面板 */}
        {open && (
          <div
            className={cn(
              'absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-md border border-[var(--color-border-secondary)]',
              'bg-[var(--color-surface-secondary)] py-1 shadow-lg',
              'scrollbar-thin',
            )}
            role="listbox"
          >
            {placeholder && (
              <div
                role="option"
                aria-selected={!displayLabel}
                onClick={() => {
                  onChange?.({ target: { value: '', name } })
                  setOpen(false)
                }}
                className={cn(
                  'flex cursor-pointer items-center justify-between px-3 py-1.5 text-sm',
                  highlightIdx === -1
                    ? 'bg-[var(--color-surface-hover)] text-[var(--color-text-primary)]'
                    : 'text-[var(--color-text-tertiary)]',
                )}
                onMouseEnter={() => setHighlightIdx(-1)}
              >
                <span>{placeholder}</span>
                {!displayLabel && <Check className="h-3.5 w-3.5 text-[var(--color-brand-primary)]" />}
              </div>
            )}
            {options.map((opt, idx) => (
              <div
                key={opt.value}
                role="option"
                aria-selected={opt.value === value}
                aria-disabled={opt.disabled}
                onClick={() => selectOption(opt)}
                onMouseEnter={() => !opt.disabled && setHighlightIdx(idx)}
                className={cn(
                  'flex items-center justify-between px-3 py-1.5 text-sm',
                  opt.disabled
                    ? 'cursor-not-allowed text-[var(--color-text-tertiary)] opacity-50'
                    : 'cursor-pointer',
                  !opt.disabled && highlightIdx === idx && 'bg-[var(--color-surface-hover)]',
                  !opt.disabled && opt.value === value
                    ? 'text-[var(--color-brand-primary)]'
                    : 'text-[var(--color-text-primary)]',
                )}
              >
                <span className="truncate">{opt.label}</span>
                {opt.value === value && <Check className="h-3.5 w-3.5 shrink-0" />}
              </div>
            ))}
            {options.length === 0 && !placeholder && (
              <div className="px-3 py-2 text-sm text-[var(--color-text-tertiary)]">无选项</div>
            )}
          </div>
        )}
      </div>
    )
  },
)

Select.displayName = 'Select'
