// REQ-U-004: 系统资源 — 使用 /api/system/status 端点 + 服务资源汇总
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { t } from '@/lib/i18n'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import { Skeleton } from '@/components/ui/Skeleton'
import { Cpu, MemoryStick, HardDrive, Box, AlertTriangle, Server, Activity, Clock } from 'lucide-react'
import type { ServicesResponse } from '@/types/service'

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

// 定时任务条目（GET /api/cron 返回，与 CronTasks.tsx 一致）
interface CronEntry {
  extension_name: string
  action_id: string
  schedule: string
  next_run?: string
  service?: string
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

  // 服务资源汇总：复用 ServiceOverview 的 queryKey（共享缓存，不产生额外请求）
  const { data: servicesData } = useQuery({
    queryKey: ['services-list'],
    queryFn: () => apiGet<ServicesResponse>('/api/services', undefined, true),
    refetchInterval: 5_000,
  })

  // 定时任务：复用 CronTasks 的 queryKey（共享缓存）
  const { data: cronData } = useQuery({
    queryKey: ['cron'],
    queryFn: () => apiGet<CronEntry[] | null>('/api/cron', undefined, true),
    refetchInterval: 10_000,
  })

  const services = servicesData?.services ?? []
  // 简单求和：将服务列表中的 CPU/内存占用累加
  const serviceCpuTotal = services.reduce((sum, s) => sum + (s.cpu_percent ?? 0), 0)
  const serviceMemoryTotal = services.reduce((sum, s) => sum + (s.memory_mb ?? 0), 0)
  // 正在运行的服务：状态为 up 或 ready（进程存活）
  const runningServices = services.filter((s) => s.status === 'up' || s.status === 'ready').length
  const cronEntries = cronData ?? []

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

        {/* 服务资源汇总 — 求和所有服务的 CPU/内存 + 运行状态统计 */}
        <div className="mt-4 pt-4 border-t border-[var(--color-border-secondary)]">
          <div className="mb-3 flex items-center gap-2">
            <Server className="h-4 w-4 text-[var(--color-text-tertiary)]" />
            <span className="text-sm font-medium text-[var(--color-text-secondary)]">服务资源汇总</span>
            <span className="text-xs text-[var(--color-text-tertiary)]">({services.length} 个服务)</span>
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {/* 服务 CPU 总占用 */}
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm">
                <Cpu className="h-4 w-4 text-[var(--color-brand-primary)]" />
                <span className="text-[var(--color-text-secondary)]">服务 CPU 总占用</span>
              </div>
              {isLoading ? (
                <Skeleton className="h-6 w-20" />
              ) : (
                <div className="text-xl font-semibold text-[var(--color-text-primary)]">
                  {services.length > 0 ? `${serviceCpuTotal.toFixed(1)}%` : '-'}
                </div>
              )}
              <div className="text-xs text-[var(--color-text-tertiary)]">所有服务进程求和</div>
            </div>

            {/* 服务内存总占用 */}
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm">
                <MemoryStick className="h-4 w-4 text-[var(--color-text-success)]" />
                <span className="text-[var(--color-text-secondary)]">服务内存总占用</span>
              </div>
              {isLoading ? (
                <Skeleton className="h-6 w-20" />
              ) : (
                <div className="text-xl font-semibold text-[var(--color-text-primary)]">
                  {services.length > 0 ? formatMB(serviceMemoryTotal) : '-'}
                </div>
              )}
              <div className="text-xs text-[var(--color-text-tertiary)]">所有服务进程求和</div>
            </div>

            {/* 正在运行服务 */}
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm">
                <Activity className="h-4 w-4 text-[var(--color-text-info)]" />
                <span className="text-[var(--color-text-secondary)]">正在运行服务</span>
              </div>
              {isLoading ? (
                <Skeleton className="h-6 w-16" />
              ) : (
                <div className="text-xl font-semibold text-[var(--color-text-primary)]">
                  {services.length > 0 ? `${runningServices} / ${services.length}` : '-'}
                </div>
              )}
              <div className="text-xs text-[var(--color-text-tertiary)]">up / ready 状态</div>
            </div>

            {/* 定时任务数 */}
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm">
                <Clock className="h-4 w-4 text-[var(--color-accent-warning)]" />
                <span className="text-[var(--color-text-secondary)]">定时任务数</span>
              </div>
              {isLoading ? (
                <Skeleton className="h-6 w-16" />
              ) : (
                <div className="text-xl font-semibold text-[var(--color-text-primary)]">
                  {cronEntries.length}
                </div>
              )}
              <div className="text-xs text-[var(--color-text-tertiary)]">已注册调度</div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
