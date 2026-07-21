// REQ-U-008: 事件流页面
// 实时事件流（长轮询）+ 14种事件类型分类筛选

import { useState, useEffect, useCallback, useRef } from 'react'
import { apiLongPoll, ApiException } from '@/lib/api-client'
import { EventCard, type EventItem } from '@/components/event/EventCard'
import { EventTypeFilter } from '@/components/event/EventTypeFilter'
import { Button } from '@/components/ui/Button'
import { t } from '@/lib/i18n'
import { Radio, Pause, Play, RefreshCw, AlertTriangle } from 'lucide-react'

interface ApiEventData {
  time: string
  type: string
  payload: Record<string, unknown>
}

interface ApiEventsResponse {
  data: ApiEventData[]
  next_since: string
  has_more: boolean
}

let eventCounter = 0

function apiEventToEventItem(e: ApiEventData): EventItem {
  return {
    id: `evt-${++eventCounter}-${e.time}`,
    type: e.type,
    timestamp: e.time,
    payload: e.payload,
  }
}

export default function EventsPage() {
  const [events, setEvents] = useState<EventItem[]>([])
  const [selectedTypes, setSelectedTypes] = useState<Set<string>>(new Set())
  const [isStreaming, setIsStreaming] = useState(true)
  const [since, setSince] = useState('')
  const [connectionError, setConnectionError] = useState(false)
  // F-02-001: 503 SERVICE_BUSY 友好提示状态
  const [serviceBusy, setServiceBusy] = useState(false)
  const abortRef = useRef<AbortController | null>(null)

  const fetchEvents = useCallback(async () => {
    if (!isStreaming) return

    const controller = new AbortController()
    abortRef.current = controller

    try {
      const params: Record<string, string | number | boolean | undefined> = {
        wait: 30,
        limit: 50,
      }
      if (since) {
        params.since = since
      }
      if (selectedTypes.size > 0 && selectedTypes.size < 14) {
        params.types = Array.from(selectedTypes).join(',')
      }

      const resp = await apiLongPoll<ApiEventsResponse>(
        '/api/events',
        params,
        controller.signal,
      )

      setConnectionError(false)
      if (resp.data && resp.data.length > 0) {
        const items = resp.data.map(apiEventToEventItem)
        setEvents((prev) => [...items, ...prev].slice(0, 200))
      }
      if (resp.next_since) {
        setSince(resp.next_since)
      }
    } catch (err) {
      // E-02-003 修复：区分用户主动 abort 和网络错误
      if (controller.signal.aborted) return
      // F-02-001: 识别 503 SERVICE_BUSY，显示友好提示而非"连接中断"
      const isServiceBusy = err instanceof ApiException && (err.code === 'SERVICE_BUSY' || err.status === 503)
      if (isServiceBusy) {
        setServiceBusy(true)
      } else {
        setConnectionError(true)
      }
    }
  }, [isStreaming, since, selectedTypes])

  useEffect(() => {
    fetchEvents()
  }, [fetchEvents])

  // E-02-003 修复：连接出错时 5 秒后自动重试
  useEffect(() => {
    if (!connectionError || !isStreaming) return
    const timer = setTimeout(() => {
      setConnectionError(false)
      fetchEvents()
    }, 5000)
    return () => clearTimeout(timer)
  }, [connectionError, isStreaming, fetchEvents])

  // F-02-001: SERVICE_BUSY 时 5 秒后自动重试
  useEffect(() => {
    if (!serviceBusy || !isStreaming) return
    const timer = setTimeout(() => {
      setServiceBusy(false)
      fetchEvents()
    }, 5000)
    return () => clearTimeout(timer)
  }, [serviceBusy, isStreaming, fetchEvents])

  useEffect(() => {
    return () => {
      abortRef.current?.abort()
    }
  }, [])

  function toggleStreaming() {
    if (isStreaming) {
      abortRef.current?.abort()
      setIsStreaming(false)
    } else {
      setIsStreaming(true)
    }
  }

  const filteredEvents = selectedTypes.size === 0
    ? events
    : events.filter((e) => selectedTypes.has(e.type))

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">{t.events.title}</h1>
        <div className="flex items-center gap-2">
          <Button variant="default" size="sm" onClick={toggleStreaming}>
            {isStreaming ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
            {isStreaming ? t.events.pause : t.events.resume}
          </Button>
          <Button variant="default" size="sm" onClick={() => { setEvents([]); setSince('') }}>
            <RefreshCw className="h-4 w-4" />
            {t.events.clear}
          </Button>
        </div>
      </div>

      {/* 事件类型筛选 */}
      <EventTypeFilter selected={selectedTypes} onChange={setSelectedTypes} />

      {/* 实时状态指示 */}
      {isStreaming && !connectionError && (
        <div className="flex items-center gap-1.5 text-xs text-[var(--color-text-tertiary)]">
          <span className="relative flex h-2 w-2">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-[var(--color-brand-primary)] opacity-75" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-[var(--color-brand-primary)]" />
          </span>
          {t.events.live}
        </div>
      )}
      {isStreaming && connectionError && (
        <div className="flex items-center gap-1.5 text-xs text-[var(--color-text-warning)]">
          <AlertTriangle className="h-3.5 w-3.5" />
          连接中断，5 秒后自动重试...
        </div>
      )}
      {isStreaming && serviceBusy && (
        <div className="flex items-center gap-1.5 text-xs text-[var(--color-text-warning)]">
          <AlertTriangle className="h-3.5 w-3.5" />
          服务正忙，请稍后重试，5 秒后自动重试...
        </div>
      )}

      {/* 事件流列表 */}
      <div className="space-y-2">
        {filteredEvents.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 text-sm text-[var(--color-text-secondary)]">
            <Radio className="h-8 w-8 mb-2 text-[var(--color-text-tertiary)]" />
            <p>{t.events.empty}</p>
          </div>
        ) : (
          filteredEvents.map((event) => (
            <EventCard key={event.id} event={event} />
          ))
        )}
      </div>
    </div>
  )
}
