// REQ-U-004: 服务状态总览 — 总数+各状态分布+服务表格
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { t } from '@/lib/i18n'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { SkeletonTable } from '@/components/ui/Skeleton'
import { Link } from 'react-router'
import { IconRenderer } from '@/components/common/IconRenderer'
import { useHTTPProbe, sortAndLimitPorts, type ProbedPortInfo } from '@/lib/http-probe'
import { AlertTriangle } from 'lucide-react'
import type { ServiceItem, ServicesResponse, ServiceState } from '@/types/service'

// I-03-001 修复：ServiceState / ServiceItem / ServicesResponse 抽取到 @/types/service
// 保留 re-export 以兼容现有 import { ServiceState } from '@/components/dashboard/ServiceOverview'
export type { ServiceState } from '@/types/service'

const stateVariantMap: Record<ServiceState, 'default' | 'info' | 'success' | 'warning' | 'danger' | 'secondary'> = {
  pending: 'secondary',
  starting: 'info',
  up: 'success',
  ready: 'success',
  stopping: 'warning',
  down: 'secondary',
  failed: 'danger',
}

function formatUptime(seconds: number): string {
  if (seconds <= 0) return '-'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}天${h}时`
  if (h > 0) return `${h}时${m}分`
  return `${m}分`
}

export function ServiceOverview() {
  // E-01-001: silent=true 避免轮询错误时弹出 toast，由内联错误横幅提示
  const { data, isLoading, isError } = useQuery({
    queryKey: ['services-list'],
    queryFn: () => apiGet<ServicesResponse>('/api/services', undefined, true),
    refetchInterval: 5_000, // G-03: 服务列表短轮询 5s（降低高频请求压力）
  })

  const services = data?.services ?? []
  const total = services.length

  // 稳定排序：活跃优先，同状态按名称排
  const statusOrder: Record<ServiceState, number> = {
    ready: 0, up: 1, starting: 2, stopping: 3, failed: 4, down: 5, pending: 6,
  }
  const sortedServices = [...services].sort((a, b) => {
    const sa = statusOrder[a.status] ?? 9
    const sb = statusOrder[b.status] ?? 9
    if (sa !== sb) return sa - sb
    return a.name.localeCompare(b.name)
  })

  const statusCounts: Record<string, number> = {}
  for (const s of services) {
    statusCounts[s.status] = (statusCounts[s.status] ?? 0) + 1
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle>{t.dashboard.serviceOverview}</CardTitle>
          <span className="text-sm text-[var(--color-text-tertiary)]">
            {t.dashboard.totalServices}: <span className="font-semibold text-[var(--color-text-primary)]">{total}</span>
          </span>
        </div>
      </CardHeader>
      <CardContent>
        {/* 状态分布 */}
        <div className="mb-4 flex flex-wrap gap-2">
          {(['pending', 'starting', 'up', 'ready', 'stopping', 'down', 'failed'] as ServiceState[]).map((state) => {
            const count = statusCounts[state] ?? 0
            if (count === 0) return null
            return (
              <Badge key={state} variant={stateVariantMap[state]}>
                {t.status[state]} {count}
              </Badge>
            )
          })}
        </div>

        {/* E-01-001: 错误横幅 */}
        {isError && (
          <div className="mb-3 flex items-center gap-2 rounded-md border border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-3 py-2 text-sm text-[var(--color-text-error)]">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            <span>服务列表加载失败，将在稍后自动重试。</span>
          </div>
        )}

        {/* E-01-001: 首次加载显示骨架屏 */}
        {isLoading ? (
          <SkeletonTable rows={5} cols={8} />
        ) : (
        /* 服务表格 */
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t.service.name}</TableHead>
              <TableHead>{t.service.status}</TableHead>
              <TableHead>端口</TableHead>
              <TableHead>CPU</TableHead>
              <TableHead>内存</TableHead>
              <TableHead>{t.service.uptime}</TableHead>
              <TableHead>{t.service.restarts}</TableHead>
              <TableHead>{t.service.tags}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sortedServices.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="text-center text-[var(--color-text-tertiary)] py-8">
                  {t.common.noData}
                </TableCell>
              </TableRow>
            ) : (
              sortedServices.map((svc) => (
                <DashboardServiceRow key={svc.name} svc={svc} />
              ))
            )}
          </TableBody>
        </Table>
        )}
      </CardContent>
    </Card>
  )
}

// 首页服务行：独立 useHTTPProbe
function DashboardServiceRow({ svc }: { svc: ServiceItem }) {
  const probedPorts = useHTTPProbe(svc.ports)
  const displayPorts = sortAndLimitPorts(probedPorts, 3)

  return (
    <TableRow>
      <TableCell>
        <Link
          to={`/services/${svc.name}`}
          className="flex items-center gap-2 text-[var(--color-brand-primary)] hover:underline"
        >
          <IconRenderer name={svc.icon} className="h-4 w-4 text-[var(--color-brand-primary)]" />
          <span className="font-medium">{svc.name}</span>
        </Link>
      </TableCell>
      <TableCell>
        <Badge variant={stateVariantMap[svc.status]}>{t.status[svc.status]}</Badge>
      </TableCell>
      <TableCell>
        <PortBadges ports={displayPorts} total={probedPorts.length} />
      </TableCell>
      <TableCell className="text-[var(--color-text-secondary)] font-mono text-xs">
        {svc.cpu_percent != null ? `${svc.cpu_percent.toFixed(1)}%` : '-'}
      </TableCell>
      <TableCell className="text-[var(--color-text-secondary)] font-mono text-xs">
        {svc.memory_mb != null ? `${svc.memory_mb.toFixed(1)}MB` : '-'}
      </TableCell>
      <TableCell className="text-[var(--color-text-secondary)]">
        {formatUptime(svc.uptime)}
      </TableCell>
      <TableCell className="text-[var(--color-text-secondary)]">
        {svc.restart_count}
      </TableCell>
      <TableCell>
        <div className="flex gap-1">
          {(svc.tags ?? []).map((tag) => (
            <Badge key={tag} variant="secondary">{tag}</Badge>
          ))}
        </div>
      </TableCell>
    </TableRow>
  )
}

// 端口标签渲染
function PortBadges({ ports, total }: { ports: ProbedPortInfo[]; total?: number }) {
  if (!ports || ports.length === 0) return <span className="text-xs text-[var(--color-text-tertiary)]">-</span>
  const overflow = (total ?? ports.length) - ports.length
  return (
    <div className="flex flex-wrap gap-1">
      {ports.map((p) =>
        p.is_http ? (
          <a
            key={`${p.protocol}-${p.port}`}
            href={`http://127.0.0.1:${p.port}`}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-xs font-mono bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)] hover:underline"
            title={`${p.protocol}://${p.address}:${p.port} (HTTP)`}
          >
            {p.port}
          </a>
        ) : (
          <span
            key={`${p.protocol}-${p.port}`}
            className="inline-flex items-center rounded px-1.5 py-0.5 text-xs font-mono bg-[var(--color-bg-tertiary)] text-[var(--color-text-secondary)]"
            title={`${p.protocol}://${p.address}:${p.port}`}
          >
            {p.port}
          </span>
        )
      )}
      {overflow > 0 && (
        <span className="inline-flex items-center rounded px-1.5 py-0.5 text-xs text-[var(--color-text-tertiary)]">
          +{overflow}
        </span>
      )}
    </div>
  )
}
