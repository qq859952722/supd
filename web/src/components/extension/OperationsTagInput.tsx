// operations 操作 ID 标签输入（全局扩展注册操作 / 服务扩展响应绑定共用）。
// 规格约束（v1.6 §2.2.3）：元素匹配 ^[a-z][a-z0-9-]*$；同一扩展内展平后重复即配置错误。

import { useState } from 'react'
import { Plus, X } from 'lucide-react'
import { t } from '@/lib/i18n'

const OPERATION_ID_REGEX = /^[a-z][a-z0-9-]*$/

export function OperationsTagInput({
  value = [],
  onChange,
  disabled,
}: {
  value?: string[]
  onChange: (next: string[]) => void
  disabled?: boolean
}) {
  const [draft, setDraft] = useState('')
  const [err, setErr] = useState('')
  const list = value ?? []

  const add = () => {
    const id = draft.trim()
    if (!id) return
    if (!OPERATION_ID_REGEX.test(id)) {
      setErr(t.extension.operationIdInvalid)
      return
    }
    if (list.includes(id)) {
      setErr(t.extension.operationIdDuplicate)
      return
    }
    setErr('')
    setDraft('')
    onChange([...list, id])
  }

  return (
    <div>
      <div className="flex items-center gap-1.5">
        <input
          value={draft}
          placeholder={t.extension.operationIdPlaceholder}
          spellCheck={false}
          disabled={disabled}
          onChange={(e) => { setDraft(e.target.value); setErr('') }}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); add() } }}
          className="h-7 min-w-0 flex-1 rounded border border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] px-1.5 font-mono text-xs text-[var(--color-text-primary)] outline-none focus:border-[var(--color-border-focus)] disabled:opacity-50"
        />
        <button
          type="button"
          disabled={disabled}
          onClick={add}
          className="flex shrink-0 items-center gap-0.5 rounded px-1.5 py-1 text-xs text-[var(--color-brand-primary)] hover:bg-[var(--color-surface-hover)] disabled:opacity-50"
        >
          <Plus className="h-3 w-3" /> {t.extension.operationAdd}
        </button>
      </div>
      {list.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-1">
          {list.map((op, index) => (
            <span
              key={`${op}-${index}`}
              className="flex items-center gap-1 rounded bg-[var(--color-surface-secondary)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--color-text-secondary)]"
            >
              {op}
              {!disabled && (
                <button
                  type="button"
                  title={t.extension.operationRemove}
                  onClick={() => onChange(list.filter((_, idx) => idx !== index))}
                  className="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-error)]"
                >
                  <X className="h-3 w-3" />
                </button>
              )}
            </span>
          ))}
        </div>
      )}
      {err && <p className="mt-1 text-[10px] text-[var(--color-text-error)]">{err}</p>}
    </div>
  )
}
