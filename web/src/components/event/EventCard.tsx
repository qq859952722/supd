// REQ-U-008: 事件卡片组件
// 显示时间、类型、图标、payload

import { Badge } from '@/components/ui/Badge'
import { t } from '@/lib/i18n'
import {
  Activity,
  Zap,
  Server,
  CheckCircle,
  XCircle,
  AlertTriangle,
  Clock,
  RefreshCw,
  Settings,
  Cpu,
  Radio,
} from 'lucide-react'
import type { ElementType } from 'react'

export interface EventItem {
  id: string
  type: string
  timestamp: string
  payload: Record<string, unknown>
}

const eventIcons: Record<string, ElementType> = {
  service_state: Activity,
  service_died: XCircle,
  service_ready: CheckCircle,
  service_failed: AlertTriangle,
  service_exited: Server,
  extension_started: Zap,
  extension_completed: CheckCircle,
  extension_failed: XCircle,
  extension_canceled: Clock,
  extension_timeout: AlertTriangle,
  cron_triggered: Clock,
  config_reloaded: RefreshCw,
  config_reload_failed: Settings,
  system_resource_warning: Cpu,
}

const eventVariant: Record<string, 'success' | 'danger' | 'warning' | 'info' | 'default'> = {
  service_state: 'info',
  service_died: 'danger',
  service_ready: 'success',
  service_failed: 'danger',
  service_exited: 'default',
  extension_started: 'info',
  extension_completed: 'success',
  extension_failed: 'danger',
  extension_canceled: 'default',
  extension_timeout: 'warning',
  cron_triggered: 'info',
  config_reloaded: 'success',
  config_reload_failed: 'danger',
  system_resource_warning: 'warning',
}

const eventTypeLabel: Record<string, string> = {
  service_state: t.events.typeServiceState,
  service_died: t.events.typeServiceDied,
  service_ready: t.events.typeServiceReady,
  service_failed: t.events.typeServiceFailed,
  service_exited: t.events.typeServiceExited,
  extension_started: t.events.typeExtStarted,
  extension_completed: t.events.typeExtCompleted,
  extension_failed: t.events.typeExtFailed,
  extension_canceled: t.events.typeExtCanceled,
  extension_timeout: t.events.typeExtTimeout,
  cron_triggered: t.events.typeCronTriggered,
  config_reloaded: t.events.typeConfigReloaded,
  config_reload_failed: t.events.typeConfigReloadFailed,
  system_resource_warning: t.events.typeSystemWarning,
}

export function EventCard({ event }: { event: EventItem }) {
  const Icon = eventIcons[event.type] ?? Radio
  const variant = eventVariant[event.type] ?? 'default'
  const label = eventTypeLabel[event.type] ?? event.type

  return (
    <div className="flex items-start gap-3 rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-3 hover:bg-[var(--color-surface-hover)] transition-colors">
      <div className="mt-0.5 shrink-0">
        <Icon className="h-4 w-4 text-[var(--color-text-secondary)]" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <Badge variant={variant}>{label}</Badge>
          <span className="shrink-0 text-xs text-[var(--color-text-tertiary)] whitespace-nowrap">
            {event.timestamp}
          </span>
        </div>
        {event.payload && Object.keys(event.payload).length > 0 && (
          <div className="mt-1.5 text-xs text-[var(--color-text-secondary)]">
            {Object.entries(event.payload).map(([key, value]) => (
              <span key={key} className="mr-3">
                <span className="text-[var(--color-text-tertiary)]">{key}=</span>
                {String(value)}
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
