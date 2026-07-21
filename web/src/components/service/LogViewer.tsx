// REQ-U-005: 日志查看器 — 长轮询实时日志流+暂停/清屏/下载/搜索
// REQ-2.1.7: 日志搜索上限 1000 行
import { useState, useEffect, useRef, useCallback } from 'react'
import { apiLongPoll } from '@/lib/api-client'
import { t } from '@/lib/i18n'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Badge } from '@/components/ui/Badge'
import { Pause, Play, Trash2, Download, Search } from 'lucide-react'

interface LogLine {
  timestamp: number
  message: string
  level: string
  source?: 'service' | 'extension'
  extension_name?: string
}

interface LogViewerProps {
  serviceName: string
  logs?: LogLine[]
}

interface LongPollLogsResponse {
  data: Array<{ content?: string }>
  next_since: string
  has_more: boolean
}

const levelColors: Record<string, string> = {
  debug: 'text-[var(--color-text-tertiary)]',
  info: 'text-[var(--color-brand-primary)]',
  warn: 'text-[var(--color-accent-warning)]',
  error: 'text-[var(--color-text-error)]',
}

// 从日志行内容启发式识别级别
function detectLevel(content: string): string {
  const lower = content.toLowerCase()
  if (lower.includes('error') || lower.includes('err:') || lower.includes('failed')) return 'error'
  if (lower.includes('warn')) return 'warn'
  if (lower.includes('debug')) return 'debug'
  return 'info'
}

const MAX_LOG_LINES = 5000 // 前端保留上限，避免内存爆炸

export function LogViewer({ serviceName, logs: externalLogs }: LogViewerProps) {
  const [logs, setLogs] = useState<LogLine[]>(externalLogs ?? [])
  const [isPaused, setIsPaused] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [showSearch, setShowSearch] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const isAutoScroll = useRef(true)
  const sinceRef = useRef<string>('')
  const pausedRef = useRef(false)
  const abortRef = useRef<AbortController | null>(null)

  // 同步暂停状态到 ref（避免长轮询循环读到旧值）
  useEffect(() => {
    pausedRef.current = isPaused
  }, [isPaused])

  useEffect(() => {
    if (isAutoScroll.current && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight
    }
  }, [logs])

  const handleScroll = useCallback(() => {
    if (!containerRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current
    isAutoScroll.current = scrollHeight - scrollTop - clientHeight < 50
  }, [])

  // 长轮询循环：wait=true 让后端挂起最多 30s，有新日志立即返回
  // spec L1133/L1251: 实时数据用长轮询（30 秒挂起，有数据立即返回）
  useEffect(() => {
    if (!serviceName) return
    let cancelled = false

    const poll = async () => {
      while (!cancelled && !pausedRef.current) {
        const controller = new AbortController()
        abortRef.current = controller
        try {
          const params: Record<string, string | number | boolean | undefined> = {
            since: sinceRef.current,
            wait: 'true',
          }
          const res = await apiLongPoll<LongPollLogsResponse>(
            `/api/services/${encodeURIComponent(serviceName)}/logs`,
            params,
            controller.signal,
          )
          if (cancelled) break
          if (res.next_since) {
            sinceRef.current = res.next_since
          }
          const newLines: LogLine[] = (res.data ?? [])
            .filter((item) => item?.content)
            .map((item) => ({
              timestamp: Date.now() / 1000,
              message: item.content!,
              level: detectLevel(item.content!),
            }))
          if (newLines.length > 0) {
            setLogs((prev) => {
              const combined = [...prev, ...newLines]
              // 超过上限时丢弃最早的
              return combined.length > MAX_LOG_LINES
                ? combined.slice(combined.length - MAX_LOG_LINES)
                : combined
            })
          }
        } catch {
          // 错误时短暂等待后重试，避免紧密循环
          await new Promise((resolve) => setTimeout(resolve, 2_000))
        }
      }
    }

    poll()

    return () => {
      cancelled = true
      abortRef.current?.abort()
    }
  }, [serviceName])

  const handleClear = () => setLogs([])

  const handleDownload = () => {
    const content = logs
      .map((log) => {
        const time = new Date(log.timestamp * 1000).toISOString()
        return `[${time}] [${log.level.toUpperCase()}]${log.source === 'extension' ? ` [ext:${log.extension_name}]` : ''} ${log.message}`
      })
      .join('\n')
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${serviceName}-logs.txt`
    a.click()
    URL.revokeObjectURL(url)
  }

  const filteredLogs = searchQuery
    ? logs.filter((log) => log.message.toLowerCase().includes(searchQuery.toLowerCase()))
    : logs

  return (
    <div className="flex flex-col">
      {/* 工具栏 */}
      <div className="flex items-center gap-2 pb-3 border-b border-[var(--color-border-primary)]">
        <Button
          variant="default"
          size="sm"
          onClick={() => setIsPaused(!isPaused)}
        >
          {isPaused ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
          {isPaused ? t.service.resume : t.service.pause}
        </Button>
        <Button variant="default" size="sm" onClick={handleClear}>
          <Trash2 className="h-3.5 w-3.5" />
          {t.service.clearScreen}
        </Button>
        <Button variant="default" size="sm" onClick={handleDownload}>
          <Download className="h-3.5 w-3.5" />
          {t.service.download}
        </Button>
        <Button
          variant="default"
          size="sm"
          onClick={() => setShowSearch(!showSearch)}
        >
          <Search className="h-3.5 w-3.5" />
          {t.service.search}
        </Button>
        {isPaused && (
          <Badge variant="warning" className="ml-auto">已暂停</Badge>
        )}
      </div>

      {/* 搜索栏 */}
      {showSearch && (
        <div className="py-2">
          <Input
            placeholder={t.service.search}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
        </div>
      )}

      {/* 日志内容 */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-auto bg-[var(--color-bg-primary)] rounded-md border border-[var(--color-border-primary)] p-3 font-mono text-xs leading-5"
        style={{ maxHeight: '500px', minHeight: '300px' }}
      >
        {filteredLogs.length === 0 ? (
          <div className="text-center text-[var(--color-text-tertiary)] py-8">
            暂无日志记录
          </div>
        ) : (
          filteredLogs.map((log, idx) => {
            const time = new Date(log.timestamp * 1000)
            const timeStr = `${time.getHours().toString().padStart(2, '0')}:${time.getMinutes().toString().padStart(2, '0')}:${time.getSeconds().toString().padStart(2, '0')}`
            return (
              <div key={idx} className="flex gap-2">
                <span className="shrink-0 text-[var(--color-text-tertiary)]">{timeStr}</span>
                <span className={`shrink-0 uppercase ${levelColors[log.level] ?? 'text-[var(--color-text-secondary)]'}`}>
                  [{log.level}]
                </span>
                {log.source === 'extension' && (
                  <Badge variant="info" className="shrink-0 text-[10px] px-1 py-0">
                    ext:{log.extension_name}
                  </Badge>
                )}
                <span className="text-[var(--color-text-primary)] break-all">{log.message}</span>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
