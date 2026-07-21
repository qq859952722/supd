// REQ-U-004: 顶部状态条 — 运行状态+运行时长+版本+监听地址+认证模式
// P-03-01: 健康度聚合 — 根据所有服务状态显示多态健康指示
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { t } from '@/lib/i18n'
import { Badge } from '@/components/ui/Badge'
import { Activity, Clock, Hash, Globe, Shield } from 'lucide-react'
import type { ServiceItem, ServicesResponse } from '@/types/service'

interface SupdStatus {
  start_time: string
  uptime_seconds: number
  version: string
  http_listen: string
  auth_mode: string
  memory_mb?: number
}

interface HealthState {
  variant: 'success' | 'danger' | 'warning' | 'default'
  label: string
}

/** P-03-01: 根据所有服务状态聚合健康度 */
function aggregateHealth(services: Pick<ServiceItem, 'name' | 'status'>[], unreachable: boolean): HealthState {
  if (unreachable) return { variant: 'danger', label: '不可达' }
  if (services.length === 0) return { variant: 'success', label: '运行中' }
  const hasFailed = services.some((s) => s.status === 'failed')
  const hasTransition = services.some((s) => s.status === 'starting' || s.status === 'stopping' || s.status === 'pending')
  const allDown = services.every((s) => s.status === 'down')
  if (hasFailed) return { variant: 'danger', label: '有故障' }
  if (hasTransition) return { variant: 'warning', label: '过渡中' }
  if (allDown) return { variant: 'default', label: '已停止' }
  return { variant: 'success', label: '运行中' }
}

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}天${h}小时`
  if (h > 0) return `${h}小时${m}分`
  return `${m}分钟`
}

export function StatusBar() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['supd-status'],
    queryFn: () => apiGet<SupdStatus>('/api/system/status', undefined, true),
    refetchInterval: 10_000,
  })

  // P-03-01: 获取服务列表用于健康度聚合（silent 避免错误 toast 干扰）
  const { data: servicesData, isError: servicesError } = useQuery({
    queryKey: ['status-bar-services'],
    queryFn: () => apiGet<ServicesResponse>('/api/services', undefined, true),
    refetchInterval: 5_000,
  })

  const services = servicesData?.services ?? []
  const health = aggregateHealth(services, servicesError || isError)

  return (
    <div className="flex flex-wrap items-center gap-4 rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] px-4 py-3">
      <div className="flex items-center gap-2">
        <Activity className="h-4 w-4 text-[var(--color-brand-primary)]" />
        <span className="text-sm text-[var(--color-text-secondary)]">{t.dashboard.status}</span>
        {isLoading ? (
          <Badge variant="default">-</Badge>
        ) : (
          <Badge variant={health.variant}>
            <span className={`inline-block h-1.5 w-1.5 rounded-full mr-1 ${
              health.variant === 'success' ? 'bg-[var(--color-text-success)]'
                : health.variant === 'danger' ? 'bg-[var(--color-text-error)]'
                : health.variant === 'warning' ? 'bg-[var(--color-accent-warning)]'
                : 'bg-[var(--color-text-tertiary)]'
            }`} />
            {health.label}
          </Badge>
        )}
      </div>
      <div className="flex items-center gap-2">
        <Clock className="h-4 w-4 text-[var(--color-text-tertiary)]" />
        <span className="text-sm text-[var(--color-text-secondary)]">{t.dashboard.uptime}</span>
        <span className="text-sm font-medium text-[var(--color-text-primary)]">
          {data ? formatUptime(data.uptime_seconds) : '-'}
        </span>
      </div>
      <div className="flex items-center gap-2">
        <Hash className="h-4 w-4 text-[var(--color-text-tertiary)]" />
        <span className="text-sm text-[var(--color-text-secondary)]">{t.dashboard.version}</span>
        <span className="text-sm font-medium text-[var(--color-text-primary)]">{data?.version ?? '-'}</span>
      </div>
      <div className="flex items-center gap-2">
        <Globe className="h-4 w-4 text-[var(--color-text-tertiary)]" />
        <span className="text-sm text-[var(--color-text-secondary)]">{t.dashboard.listenAddr}</span>
        <span className="text-sm font-medium text-[var(--color-text-primary)] font-mono">{data?.http_listen ?? '-'}</span>
      </div>
      <div className="flex items-center gap-2">
        <Shield className="h-4 w-4 text-[var(--color-text-tertiary)]" />
        <span className="text-sm text-[var(--color-text-secondary)]">{t.dashboard.authMode}</span>
        <span className="text-sm font-medium text-[var(--color-text-primary)]">{data?.auth_mode ?? '-'}</span>
      </div>
    </div>
  )
}
