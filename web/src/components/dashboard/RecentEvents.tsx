// REQ-U-004: 事件流（最近10条）
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { t } from '@/lib/i18n'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { Radio, AlertTriangle } from 'lucide-react'

interface EventData {
  time: string
  type: string
  payload: Record<string, unknown>
}

interface EventsResponse {
  data: EventData[]
  next_since: string
  has_more: boolean
}

const eventTypeVariant: Record<string, 'default' | 'success' | 'warning' | 'danger' | 'info'> = {
  service_state: 'info',
  service_died: 'danger',
  service_ready: 'success',
  service_failed: 'danger',
  service_exited: 'warning',
  extension_started: 'info',
  extension_completed: 'success',
  extension_failed: 'danger',
  extension_canceled: 'secondary' as unknown as 'default',
  extension_timeout: 'warning',
  cron_triggered: 'info',
  config_reloaded: 'success',
  config_reload_failed: 'danger',
  system_resource_warning: 'warning',
}

function getEventVariant(type: string): 'default' | 'success' | 'warning' | 'danger' | 'info' {
  return eventTypeVariant[type] ?? 'default'
}

function formatEventTime(timeStr: string): string {
  const d = new Date(timeStr)
  return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}:${d.getSeconds().toString().padStart(2, '0')}`
}

function getServiceFromPayload(payload: Record<string, unknown>): string | undefined {
  if (payload.service && typeof payload.service === 'string') return payload.service
  if (payload.name && typeof payload.name === 'string') return payload.name
  return undefined
}

function getMessageFromPayload(payload: Record<string, unknown>): string {
  if (payload.message && typeof payload.message === 'string') return payload.message
  if (payload.error && typeof payload.error === 'string') return payload.error
  return ''
}

export function RecentEvents() {
  // E-01-001: silent=true 避免轮询错误时弹出 toast
  const { data, isLoading, isError } = useQuery({
    queryKey: ['recent-events'],
    queryFn: () => apiGet<EventsResponse>('/api/events', { limit: 10 }, true),
    refetchInterval: 5_000, // G-03: 短轮询 5s（降低高频请求压力）
  })

  const events = data?.data ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t.dashboard.recentEvents}</CardTitle>
      </CardHeader>
      <CardContent>
        {isError && (
          <div className="mb-3 flex items-center gap-2 rounded-md border border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-3 py-2 text-sm text-[var(--color-text-error)]">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            <span>事件流加载失败，将在稍后自动重试。</span>
          </div>
        )}
        {isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-12 w-full" />
            ))}
          </div>
        ) : events.length === 0 ? (
          <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
            <Radio className="mr-2 h-4 w-4" />
            {t.dashboard.noEvents}
          </div>
        ) : (
          <div className="space-y-2">
            {events.map((event, idx) => {
              const service = getServiceFromPayload(event.payload)
              const message = getMessageFromPayload(event.payload)
              return (
                <div
                  key={`${event.time}-${idx}`}
                  className="flex items-start gap-3 rounded-md border border-[var(--color-border-primary)] bg-[var(--color-surface-primary)] p-2.5"
                >
                  <span className="shrink-0 text-xs font-mono text-[var(--color-text-tertiary)] pt-0.5">
                    {formatEventTime(event.time)}
                  </span>
                  <Badge variant={getEventVariant(event.type)} className="shrink-0">
                    {event.type}
                  </Badge>
                  <div className="min-w-0 flex-1">
                    {service && (
                      <span className="text-xs font-mono text-[var(--color-brand-primary)] mr-2">
                        [{service}]
                      </span>
                    )}
                    <span className="text-sm text-[var(--color-text-secondary)] truncate">
                      {message}
                    </span>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
