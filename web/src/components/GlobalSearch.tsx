// REQ-U-017: 全局搜索
// 搜索框在导航栏右侧、精确匹配（不做模糊匹配）、Ctrl+K快捷键

import { useState, useEffect, useRef, useCallback } from 'react'
import { Search, X, Server, Puzzle, FileText, Radio } from 'lucide-react'
import { t } from '@/lib/i18n'

interface SearchResultItem {
  id: string
  type: 'service' | 'extension' | 'file' | 'event'
  name: string
  path?: string
  description?: string
}

interface GlobalSearchProps {
  open: boolean
  onClose: () => void
  onSearch?: (query: string) => SearchResultItem[]
  onSelect?: (item: SearchResultItem) => void
}

const typeIconMap: Record<SearchResultItem['type'], React.ComponentType<{ className?: string }>> = {
  service: Server,
  extension: Puzzle,
  file: FileText,
  event: Radio,
}

const typeLabelMap: Record<SearchResultItem['type'], string> = {
  service: t.search.typeService,
  extension: t.search.typeExtension,
  file: t.search.typeFile,
  event: t.search.typeEvent,
}

/** E-07-001: 高亮文本中匹配关键字的片段（大小写不敏感） */
function highlightMatch(text: string, query: string): React.ReactNode {
  if (!query) return text
  const lowerText = text.toLowerCase()
  const lowerQuery = query.toLowerCase()
  const nodes: React.ReactNode[] = []
  let lastIndex = 0
  let idx = lowerText.indexOf(lowerQuery, lastIndex)
  let key = 0
  while (idx >= 0) {
    if (idx > lastIndex) {
      nodes.push(text.slice(lastIndex, idx))
    }
    nodes.push(
      <mark
        key={`hl-${key++}`}
        className="rounded-sm bg-[var(--color-brand-primary)]/25 px-0.5 text-inherit"
      >
        {text.slice(idx, idx + query.length)}
      </mark>,
    )
    lastIndex = idx + query.length
    idx = lowerText.indexOf(lowerQuery, lastIndex)
  }
  if (lastIndex < text.length) {
    nodes.push(text.slice(lastIndex))
  }
  return nodes.length > 0 ? nodes : text
}

export function GlobalSearch({ open, onClose, onSearch, onSelect }: GlobalSearchProps) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResultItem[]>([])
  const [activeIndex, setActiveIndex] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // 打开时聚焦输入框
  useEffect(() => {
    if (open) {
      setTimeout(() => inputRef.current?.focus(), 50)
    } else {
      setQuery('')
      setResults([])
      setActiveIndex(-1)
    }
  }, [open])

  // 搜索（精确匹配）
  useEffect(() => {
    if (!query.trim()) {
      setResults([])
      setActiveIndex(-1)
      return
    }
    if (onSearch) {
      // 外部提供搜索逻辑
      setResults(onSearch(query.trim()))
    } else {
      // 默认空结果（等待API集成）
      setResults([])
    }
    setActiveIndex(0)
  }, [query, onSearch])

  // Ctrl+K / Cmd+K 快捷键
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        if (open) {
          onClose()
        }
        // 由App层控制打开
      }
      if (e.key === 'Escape' && open) {
        onClose()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, onClose])

  const handleSelect = useCallback(
    (item: SearchResultItem) => {
      onSelect?.(item)
      onClose()
    },
    [onSelect, onClose],
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setActiveIndex((prev) => Math.min(prev + 1, results.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setActiveIndex((prev) => Math.max(prev - 1, 0))
      } else if (e.key === 'Enter' && activeIndex >= 0 && activeIndex < results.length) {
        e.preventDefault()
        handleSelect(results[activeIndex]!)
      }
    },
    [results, activeIndex, handleSelect],
  )

  // 滚动到活跃项
  useEffect(() => {
    if (activeIndex >= 0 && listRef.current) {
      const items = listRef.current.querySelectorAll('[data-search-item]')
      items[activeIndex]?.scrollIntoView({ block: 'nearest' })
    }
  }, [activeIndex])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-[100]">
      {/* 遮罩 */}
      <div className="absolute inset-0 bg-black/50" onClick={onClose} />

      {/* 搜索面板 */}
      <div className="relative mx-auto mt-[15vh] max-w-xl">
        <div className="overflow-hidden rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-primary)] shadow-lg">
          {/* 搜索输入框 */}
          <div className="flex items-center border-b border-[var(--color-border-primary)] px-3">
            <Search className="h-4 w-4 shrink-0 text-[var(--color-text-tertiary)]" />
            <input
              ref={inputRef}
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t.search.placeholder}
              className="flex-1 border-0 bg-transparent px-3 py-3 text-sm text-[var(--color-text-primary)] placeholder:text-[var(--color-text-tertiary)] focus:outline-none"
            />
            {query && (
              <button
                onClick={() => setQuery('')}
                className="rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>

          {/* 搜索结果 */}
          {query.trim() && (
            <div ref={listRef} className="max-h-[400px] overflow-y-auto p-2">
              {results.length === 0 ? (
                <div className="py-8 text-center text-sm text-[var(--color-text-tertiary)]">
                  {t.search.noResults}
                </div>
              ) : (
                results.map((item, index) => {
                  const Icon = typeIconMap[item.type]
                  return (
                    <button
                      key={item.id}
                      data-search-item
                      onClick={() => handleSelect(item)}
                      onMouseEnter={() => setActiveIndex(index)}
                      className={`flex w-full items-center gap-3 rounded-md px-3 py-2 text-left transition-colors ${
                        index === activeIndex
                          ? 'bg-[var(--color-surface-hover)] text-[var(--color-text-primary)]'
                          : 'text-[var(--color-text-primary)] hover:bg-[var(--color-surface-secondary)]'
                      }`}
                    >
                      <Icon className="h-4 w-4 shrink-0 text-[var(--color-text-tertiary)]" />
                      <div className="flex-1 min-w-0">
                        <div className="text-sm font-medium truncate">{highlightMatch(item.name, query.trim())}</div>
                        {item.description && (
                          <div className="text-xs text-[var(--color-text-tertiary)] truncate">
                            {highlightMatch(item.description, query.trim())}
                          </div>
                        )}
                      </div>
                      <span className="shrink-0 text-xs text-[var(--color-text-tertiary)]">
                        {typeLabelMap[item.type]}
                      </span>
                    </button>
                  )
                })
              )}
            </div>
          )}

          {/* 底部提示 */}
          <div className="flex items-center justify-between border-t border-[var(--color-border-primary)] px-3 py-2 text-xs text-[var(--color-text-tertiary)]">
            <span>{t.search.hint}</span>
            <div className="flex gap-2">
              <kbd className="rounded border border-[var(--color-border-primary)] px-1">↑↓</kbd>
              <span>{t.search.navigate}</span>
              <kbd className="rounded border border-[var(--color-border-primary)] px-1">↵</kbd>
              <span>{t.search.open}</span>
              <kbd className="rounded border border-[var(--color-border-primary)] px-1">esc</kbd>
              <span>{t.search.close}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
