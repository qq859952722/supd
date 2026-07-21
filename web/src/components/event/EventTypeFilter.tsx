// REQ-U-008: 事件类型筛选组件
// 14种事件类型分类筛选

import { Button } from '@/components/ui/Button'
import { t } from '@/lib/i18n'

// 14种事件类型，锁定不可新增 (2.9.7)
const EVENT_TYPES = [
  'service_state',
  'service_died',
  'service_ready',
  'service_failed',
  'service_exited',
  'extension_started',
  'extension_completed',
  'extension_failed',
  'extension_canceled',
  'extension_timeout',
  'cron_triggered',
  'config_reloaded',
  'config_reload_failed',
  'system_resource_warning',
] as const

const categoryLabel: Record<string, string> = {
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

export function EventTypeFilter({
  selected,
  onChange,
}: {
  selected: Set<string>
  onChange: (types: Set<string>) => void
}) {
  function toggleType(type: string) {
    const next = new Set(selected)
    if (next.has(type)) {
      next.delete(type)
    } else {
      next.add(type)
    }
    onChange(next)
  }

  function selectAll() {
    onChange(new Set(EVENT_TYPES))
  }

  function clearAll() {
    onChange(new Set())
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-[var(--color-text-primary)]">{t.events.filterTitle}</span>
        <div className="flex gap-1">
          <Button variant="default" size="sm" onClick={selectAll}>
            {t.events.selectAll}
          </Button>
          <Button variant="default" size="sm" onClick={clearAll}>
            {t.events.clearAll}
          </Button>
        </div>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {EVENT_TYPES.map((type) => (
          <button
            key={type}
            onClick={() => toggleType(type)}
            className={`rounded-full px-2.5 py-1 text-xs font-medium transition-colors ${
              selected.has(type)
                ? 'bg-[var(--color-brand-primary)] text-white'
                : 'bg-[var(--color-surface-tertiary)] text-[var(--color-text-secondary)] hover:bg-[var(--color-surface-hover)]'
            }`}
          >
            {categoryLabel[type] ?? type}
          </button>
        ))}
      </div>
    </div>
  )
}
