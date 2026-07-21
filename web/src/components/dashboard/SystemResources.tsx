// REQ-U-004: 系统资源 — 使用 /api/system/status 端点
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { t } from '@/lib/i18n'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import { Skeleton } from '@/components/ui/Skeleton'
import { Cpu, MemoryStick, HardDrive, Box, AlertTriangle } from 'lucide-react'

interface SystemStatus {
  start_time: string
  version: string
  uptime_seconds: number
  http_listen: string
  auth_mode: string
  cpu_percent?: number
  memory_mb?: number
  disk_total_mb?: number
  disk_used_mb?: number
}

function formatMB(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb.toFixed(0)} MB`
}

function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const mins = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}天 ${hours}小时`
  if (hours > 0) return `${hours}小时 ${mins}分钟`
  return `${mins}分钟`
}

function ProgressBar({ value, color }: { value: number; color: string }) {
  return (
    <div className="h-2 w-full rounded-full bg-[var(--color-surface-tertiary)]">
      <div
        className={`h-2 rounded-full transition-all duration-300 ${color}`}
        style={{ width: `${Math.min(value, 100)}%` }}
      />
    </div>
  )
}

export function SystemResourcesPanel() {
  // E-01-001: silent=true 避免轮询错误时弹出 toast，由内联错误横幅提示
  const { data, isLoading, isError } = useQuery({
    queryKey: ['system-status'],
    queryFn: () => apiGet<SystemStatus>('/api/system/status', undefined, true),
    refetchInterval: 10_000, // REQ-2.9.11: 资源采集 10s
  })

  const diskPercent = data?.disk_total_mb && data?.disk_used_mb
    ? (data.disk_used_mb / data.disk_total_mb) * 100
    : undefined

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t.dashboard.systemResources}</CardTitle>
      </CardHeader>
      <CardContent>
        {isError && (
          <div className="mb-3 flex items-center gap-2 rounded-md border border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-3 py-2 text-sm text-[var(--color-text-error)]">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            <span>系统资源信息加载失败，将在稍后自动重试。</span>
          </div>
        )}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {/* CPU — supd 进程 CPU 占用 */}
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm">
              <Cpu className="h-4 w-4 text-[var(--color-brand-primary)]" />
              <span className="text-[var(--color-text-secondary)]">{t.dashboard.cpu}</span>
            </div>
            {isLoading ? (
              <Skeleton className="h-6 w-20" />
            ) : (
              <div className="text-xl font-semibold text-[var(--color-text-primary)]">
                {data?.cpu_percent != null ? `${data.cpu_percent.toFixed(1)}%` : '暂不可用'}
              </div>
            )}
            {data?.cpu_percent != null && (
              <ProgressBar value={data.cpu_percent} color="bg-[var(--color-brand-primary)]" />
            )}
            <div className="text-xs text-[var(--color-text-tertiary)]">supd 进程</div>
          </div>

          {/* 内存 — supd 进程 RSS 内存 */}
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm">
              <MemoryStick className="h-4 w-4 text-[var(--color-text-success)]" />
              <span className="text-[var(--color-text-secondary)]">{t.dashboard.memory}</span>
            </div>
            {isLoading ? (
              <Skeleton className="h-6 w-20" />
            ) : (
              <div className="text-xl font-semibold text-[var(--color-text-primary)]">
                {data?.memory_mb != null ? formatMB(data.memory_mb) : '暂不可用'}
              </div>
            )}
            <div className="text-xs text-[var(--color-text-tertiary)]">supd 进程 RSS</div>
          </div>

          {/* 磁盘 — 工作目录所在分区占用 */}
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm">
              <HardDrive className="h-4 w-4 text-[var(--color-accent-warning)]" />
              <span className="text-[var(--color-text-secondary)]">{t.dashboard.disk}</span>
            </div>
            {isLoading ? (
              <Skeleton className="h-6 w-20" />
            ) : (
              <div className="text-xl font-semibold text-[var(--color-text-primary)]">
                {diskPercent != null ? `${diskPercent.toFixed(1)}%` : '暂不可用'}
              </div>
            )}
            {diskPercent != null && (
              <ProgressBar value={diskPercent} color="bg-[var(--color-accent-warning)]" />
            )}
            <div className="text-xs text-[var(--color-text-tertiary)]">
              {data?.disk_used_mb != null && data?.disk_total_mb != null
                ? `${formatMB(data.disk_used_mb)} / ${formatMB(data.disk_total_mb)}`
                : '-'}
            </div>
          </div>

          {/* 运行时间 */}
          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm">
              <Box className="h-4 w-4 text-[var(--color-text-info)]" />
              <span className="text-[var(--color-text-secondary)]">运行时间</span>
            </div>
            {isLoading ? (
              <Skeleton className="h-6 w-24" />
            ) : (
              <div className="text-xl font-semibold text-[var(--color-text-primary)]">
                {data ? formatUptime(data.uptime_seconds) : '-'}
              </div>
            )}
            <div className="text-xs text-[var(--color-text-tertiary)]">
              {data ? `v${data.version}` : '-'}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
