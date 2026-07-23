// REQ-U-005: 服务表格 — 名称/状态/运行时长/重启次数/icon/tags
import type { ServiceState } from '@/components/dashboard/ServiceOverview'
import { t } from '@/lib/i18n'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { Link } from 'react-router'
import { IconRenderer } from '@/components/common/IconRenderer'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { Play, Square, RotateCw, FileText, X, Loader2, Terminal, Eraser, XCircle } from 'lucide-react'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { useHTTPProbe, sortAndLimitPorts, type ProbedPortInfo } from '@/lib/http-probe'
import { useServiceActions } from '@/hooks/useServiceActions'

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

export interface ServiceTableItem {
  name: string
  status: ServiceState
  uptime: number
  restart_count: number
  icon?: string
  tags?: string[]
  cpu_percent?: number
  memory_mb?: number
  ports?: Array<{ protocol: string; port: number; address: string; state: string; is_http: boolean }>
}

interface ServiceTableProps {
  services: ServiceTableItem[]
}

// 服务操作按钮组
function ServiceActions({ name, status }: { name: string; status: ServiceState }) {
  const isRunning = status === 'up' || status === 'ready' || status === 'starting'
  const isStopped = status === 'pending' || status === 'down' || status === 'failed'

  // I-03-001 修复：5 个服务操作 mutation 提取到 useServiceActions hook（与 ServiceCard 共用）
  const { startMutation, stopMutation, restartMutation, clearFailedMutation, forceStopMutation } = useServiceActions(name)

  return (
    <div className="flex items-center gap-1.5">
      {isStopped && (
        <Button variant="primary" size="sm" onClick={() => startMutation.mutate()} disabled={startMutation.isPending}>
          {startMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
          启动
        </Button>
      )}
      {isRunning && (
        <Button variant="danger" size="sm" onClick={() => stopMutation.mutate()} disabled={stopMutation.isPending}>
          {stopMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Square className="h-3.5 w-3.5" />}
          停止
        </Button>
      )}
      {isRunning && (
        <Button variant="default" size="sm" onClick={() => restartMutation.mutate()} disabled={restartMutation.isPending}>
          {restartMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCw className="h-3.5 w-3.5" />}
          重启
        </Button>
      )}
      {/* F-03-001: failed 状态显示"清除失败"按钮，将状态从 failed 重置为 pending */}
      {status === 'failed' && (
        <Button variant="default" size="sm" onClick={() => clearFailedMutation.mutate()} disabled={clearFailedMutation.isPending} title="清除失败状态，将状态重置为 pending">
          {clearFailedMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Eraser className="h-3.5 w-3.5" />}
          清除失败
        </Button>
      )}
      {/* F-03-002: stopping 状态卡住时提供"强制停止"按钮 */}
      {status === 'stopping' && (
        <Button variant="danger" size="sm" onClick={() => forceStopMutation.mutate()} disabled={forceStopMutation.isPending} title="服务停止超时，发送 SIGKILL 强制终止">
          {forceStopMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <XCircle className="h-3.5 w-3.5" />}
          强制停止
        </Button>
      )}
    </div>
  )
}

// 服务运行历史和日志对话框
function ServiceHistoryDialog({ serviceName, onClose }: { serviceName: string; onClose: () => void }) {
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null)

  // 获取服务级扩展的运行历史（有运行中的任务时2秒轮询）
  const { data: historyData, isLoading } = useQuery({
    queryKey: ['service-ext-runs', serviceName],
    queryFn: () => apiGet<Array<{
      run_id: string
      extension_name: string
      action_id: string
      state: string
      exit_code: number
      progress: number
      result_msg: string
      result_level: string
      started_at: string
      finished_at: string
      trigger_type: string
    }>>('/api/extensions/runs', { service_name: serviceName, limit: 50 }),
    enabled: !!serviceName,
    refetchInterval: (query) => {
      const runs = query.state.data
      if (Array.isArray(runs) && runs.some((r) => r.state === 'running' || r.state === 'pending')) {
        return 2000
      }
      return false
    },
  })

  // 获取选中运行的日志（运行中的任务也实时轮询）
  const { data: logsData, isLoading: loadingLogs } = useQuery({
    queryKey: ['run-logs', selectedRunId],
    queryFn: async () => {
      const resp = await apiGet<{ lines: string[]; next_pos: number; has_more: boolean }>(
        `/api/extensions/runs/${encodeURIComponent(selectedRunId!)}/logs`,
        { since_pos: 0, wait: 'true' },
      )
      return resp
    },
    enabled: !!selectedRunId,
    refetchInterval: (query) => {
      const data = query.state.data
      if (data?.has_more) return 1000
      return false
    },
  })

  const runs = Array.isArray(historyData) ? historyData : []

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative z-10 w-full max-w-4xl max-h-[80vh] flex flex-col rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] shadow-md">
        <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-5 py-3">
          <div className="flex items-center gap-2">
            <FileText className="h-4 w-4 text-[var(--color-text-tertiary)]" />
            <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">{serviceName} — 运行历史</h3>
          </div>
          <button onClick={onClose} className="rounded-sm p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="flex-1 overflow-auto p-4 space-y-3">
          {isLoading ? (
            <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
              <Loader2 className="h-4 w-4 animate-spin mr-2" />{t.common.loading}
            </div>
          ) : runs.length > 0 ? (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>扩展</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>进度</TableHead>
                    <TableHead>启动时间</TableHead>
                    <TableHead>结束时间</TableHead>
                    <TableHead>结果</TableHead>
                    <TableHead>操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {runs.map((run) => (
                    <TableRow key={run.run_id}>
                      <TableCell className="font-medium text-xs">{run.extension_name}</TableCell>
                      <TableCell className="font-mono text-xs text-[var(--color-text-secondary)]">{run.action_id || '-'}</TableCell>
                      <TableCell>
                        {/* F-04-002 修复：timeout/canceled → warning（与 ServiceCard 一致） */}
                        <Badge variant={run.state === 'success' ? 'success' : run.state === 'failed' || run.state === 'killed' ? 'danger' : 'warning'}>
                          {run.state === 'running' || run.state === 'pending' ? (
                            <span className="flex items-center gap-1">
                              <Loader2 className="h-3 w-3 animate-spin" />
                              {run.state}
                            </span>
                          ) : run.state}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs font-mono">
                        {run.state === 'running' || run.state === 'pending' ? `${run.progress ?? 0}%` : '-'}
                      </TableCell>
                      <TableCell className="text-xs whitespace-nowrap">{run.started_at ? new Date(run.started_at).toLocaleString('zh-CN') : '-'}</TableCell>
                      <TableCell className="text-xs whitespace-nowrap">{run.finished_at ? new Date(run.finished_at).toLocaleString('zh-CN') : '-'}</TableCell>
                      <TableCell className="text-xs text-[var(--color-text-secondary)] max-w-xs truncate" title={run.result_msg || ''}>
                        {run.result_msg || '-'}
                      </TableCell>
                      <TableCell>
                        <Button variant="default" size="sm" onClick={() => setSelectedRunId(run.run_id)}>
                          <FileText className="h-3.5 w-3.5" />日志
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {selectedRunId && (
                <div className="rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] p-3">
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-xs font-medium text-[var(--color-text-secondary)]">日志输出 — {selectedRunId.slice(0, 8)}</span>
                    <button onClick={() => setSelectedRunId(null)} className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]">关闭日志</button>
                  </div>
                  {loadingLogs ? (
                    <div className="flex items-center justify-center py-4 text-xs text-[var(--color-text-tertiary)]">
                      <Loader2 className="h-3.5 w-3.5 animate-spin mr-2" />加载中...
                    </div>
                  ) : (logsData?.lines ?? []).length > 0 ? (
                    <pre className="whitespace-pre-wrap break-all text-xs font-mono text-[var(--color-text-secondary)] leading-relaxed max-h-64 overflow-auto">
                      {(logsData?.lines ?? []).join('\n')}
                    </pre>
                  ) : (
                    <div className="py-4 text-center text-xs text-[var(--color-text-tertiary)]">暂无日志</div>
                  )}
                </div>
              )}
            </>
          ) : (
            <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">暂无运行历史</div>
          )}
        </div>
      </div>
    </div>
  )
}

export function ServiceTable({ services }: ServiceTableProps) {
  const [historyService, setHistoryService] = useState<string | null>(null)
  const navigate = useNavigate()

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t.service.icon}</TableHead>
            <TableHead>{t.service.name}</TableHead>
            <TableHead>{t.service.status}</TableHead>
            <TableHead>端口</TableHead>
            <TableHead>CPU</TableHead>
            <TableHead>内存</TableHead>
            <TableHead>{t.service.uptime}</TableHead>
            <TableHead>{t.service.restarts}</TableHead>
            <TableHead>{t.service.tags}</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {services.length === 0 ? (
            <TableRow>
              <TableCell colSpan={10} className="text-center text-[var(--color-text-tertiary)] py-8">
                {t.common.noData}
              </TableCell>
            </TableRow>
          ) : (
            services.map((svc) => (
              <ServiceTableRow key={svc.name} svc={svc} onHistory={() => setHistoryService(svc.name)} onNavigate={() => navigate(`/services/${encodeURIComponent(svc.name)}?tab=logs`)} />
            ))
          )}
        </TableBody>
      </Table>
      {historyService && (
        <ServiceHistoryDialog serviceName={historyService} onClose={() => setHistoryService(null)} />
      )}
    </>
  )
}

// 单行组件：独立 useHTTPProbe，避免整个表格重渲染
function ServiceTableRow({ svc, onHistory, onNavigate }: { svc: ServiceTableItem; onHistory: () => void; onNavigate: () => void }) {
  const probedPorts = useHTTPProbe(svc.ports)
  const displayPorts = sortAndLimitPorts(probedPorts, 3)

  return (
    <TableRow>
      <TableCell>
        <IconRenderer name={svc.icon} className="h-4 w-4 text-[var(--color-brand-primary)]" />
      </TableCell>
      <TableCell>
        <Link
          to={`/services/${svc.name}`}
          className="font-medium text-[var(--color-brand-primary)] hover:underline"
        >
          {svc.name}
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
      <TableCell>
        <div className="flex items-center justify-end gap-1.5">
          <ServiceActions name={svc.name} status={svc.status} />
          <Button variant="default" size="sm" onClick={onNavigate}>
            <Terminal className="h-3.5 w-3.5" />
            日志
          </Button>
          <Button variant="default" size="sm" onClick={onHistory}>
            <FileText className="h-3.5 w-3.5" />
            历史
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

// 端口标签渲染组件：HTTP 端口蓝色可点击，非 HTTP 端口灰色，限制3个，超出显示 +N
function PortBadges({ ports, total }: { ports: ProbedPortInfo[]; total?: number }) {
  if (!ports || ports.length === 0) return <span className="text-xs text-[var(--color-text-tertiary)]">-</span>
  const overflow = (total ?? ports.length) - ports.length
  return (
    <div className="flex flex-wrap gap-1">
      {ports.map((p) =>
        p.is_http ? (
          <a
            key={`${p.protocol}-${p.port}`}
            href={`http://${window.location.hostname}:${p.port}`}
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
