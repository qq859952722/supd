// REQ-U-005: 服务卡片 — 名称/状态/运行时长/重启次数/icon/tags
import type { ServiceState } from '@/components/dashboard/ServiceOverview'
import { t } from '@/lib/i18n'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Card, CardContent } from '@/components/ui/Card'
import { Link } from 'react-router'
import { IconRenderer } from '@/components/common/IconRenderer'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { Play, Square, RotateCw, FileText, X, Loader2, Eraser, XCircle } from 'lucide-react'
import { useState } from 'react'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { useHTTPProbe, sortAndLimitPorts } from '@/lib/http-probe'
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

interface ServiceCardProps {
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

export function ServiceCard({ name, status, uptime, restart_count, icon, tags, cpu_percent, memory_mb, ports }: ServiceCardProps) {
  const [showHistory, setShowHistory] = useState(false)
  const isRunning = status === 'up' || status === 'ready' || status === 'starting'
  const isStopped = status === 'pending' || status === 'down' || status === 'failed'
  const probedPorts = useHTTPProbe(ports)
  const displayPorts = sortAndLimitPorts(probedPorts, 3)

  // I-03-001 修复：5 个服务操作 mutation 提取到 useServiceActions hook（与 ServiceTable 共用）
  const { startMutation, stopMutation, restartMutation, clearFailedMutation, forceStopMutation } = useServiceActions(name)

  return (
    <>
      <Card className="transition-all duration-150 hover:border-[var(--color-border-focus)] hover:shadow-[var(--shadow-md)]">
        <CardContent className="p-4">
          <div className="flex items-start justify-between">
            <Link to={`/services/${name}`} className="flex items-center gap-2">
              <IconRenderer name={icon} className="h-5 w-5 text-[var(--color-brand-primary)]" />
              <span className="font-medium text-[var(--color-text-primary)] hover:underline">{name}</span>
            </Link>
            <Badge variant={stateVariantMap[status]}>{t.status[status]}</Badge>
          </div>
          <div className="mt-3 flex items-center gap-4 text-xs text-[var(--color-text-tertiary)]">
            <span>{t.service.uptime}: {formatUptime(uptime)}</span>
            <span>{t.service.restarts}: {restart_count}</span>
            {cpu_percent != null && <span>CPU: {cpu_percent.toFixed(1)}%</span>}
            {memory_mb != null && <span>内存: {memory_mb.toFixed(1)}MB</span>}
          </div>
          {/* 端口展示：最多3个，HTTP优先，前端浏览器探测判定 */}
          {displayPorts.length > 0 && (
            <div className="mt-1.5 flex flex-wrap gap-1">
              {displayPorts.map((p) =>
                p.is_http ? (
                  <a
                    key={`${p.protocol}-${p.port}`}
                    href={`http://${window.location.hostname}:${p.port}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center rounded px-1.5 py-0.5 text-xs font-mono bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)] hover:underline"
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
              {probedPorts.length > 3 && (
                <span className="inline-flex items-center rounded px-1.5 py-0.5 text-xs text-[var(--color-text-tertiary)]">
                  +{probedPorts.length - 3}
                </span>
              )}
            </div>
          )}
          {tags && tags.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1">
              {tags.map((tag) => (
                <Badge key={tag} variant="secondary" className="text-xs">{tag}</Badge>
              ))}
            </div>
          )}
          {/* 操作按钮 */}
          <div className="mt-3 flex items-center gap-1.5 border-t border-[var(--color-border-secondary)] pt-2">
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
            <Button variant="default" size="sm" onClick={() => setShowHistory(true)}>
              <FileText className="h-3.5 w-3.5" />
              历史
            </Button>
          </div>
        </CardContent>
      </Card>
      {showHistory && (
        <ServiceCardHistoryDialog serviceName={name} onClose={() => setShowHistory(false)} />
      )}
    </>
  )
}

// 服务卡片的历史日志对话框
function ServiceCardHistoryDialog({ serviceName, onClose }: { serviceName: string; onClose: () => void }) {
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null)

  const { data: historyData, isLoading } = useQuery({
    queryKey: ['service-ext-runs-card', serviceName],
    queryFn: () => apiGet<Array<{
      run_id: string
      extension_name: string
      action_id: string
      state: string
      exit_code: number
      started_at: string
      finished_at: string
    }>>('/api/extensions/runs', { service: serviceName }),
    enabled: !!serviceName,
  })

  const { data: logsData, isLoading: loadingLogs } = useQuery({
    queryKey: ['run-logs-card', selectedRunId],
    queryFn: async () => {
      const resp = await apiGet<{ lines: string[]; next_pos: number; has_more: boolean }>(
        `/api/extensions/runs/${encodeURIComponent(selectedRunId!)}/logs`,
        { since_pos: 0 },
      )
      return resp
    },
    enabled: !!selectedRunId,
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
                    <TableHead>启动时间</TableHead>
                    <TableHead>结束时间</TableHead>
                    <TableHead>退出码</TableHead>
                    <TableHead>操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {runs.map((run) => (
                    <TableRow key={run.run_id}>
                      <TableCell className="font-medium text-xs">{run.extension_name}</TableCell>
                      <TableCell className="font-mono text-xs text-[var(--color-text-secondary)]">{run.action_id || '-'}</TableCell>
                      <TableCell>
                        <Badge variant={run.state === 'success' ? 'success' : run.state === 'failed' || run.state === 'killed' ? 'danger' : 'warning'}>
                          {run.state}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs whitespace-nowrap">{run.started_at ? new Date(run.started_at).toLocaleString('zh-CN') : '-'}</TableCell>
                      <TableCell className="text-xs whitespace-nowrap">{run.finished_at ? new Date(run.finished_at).toLocaleString('zh-CN') : '-'}</TableCell>
                      <TableCell className="text-xs font-mono">{run.exit_code}</TableCell>
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
