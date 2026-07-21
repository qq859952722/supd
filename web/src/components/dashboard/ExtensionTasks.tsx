// REQ-U-004: 扩展任务 — 运行中任务+最近完成+下次定时
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { t } from '@/lib/i18n'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { PlayCircle, CheckCircle2, Clock, AlertTriangle } from 'lucide-react'
import { Link } from 'react-router'

interface ExtensionSummary {
  name: string
  version: string
  description?: string
  enabled: boolean
  display_state: string
  trigger_type?: string
  run_count?: number
  success_count?: number
  fail_count?: number
  service?: string
}

interface RunEntry {
  run_id: string
  extension_name: string
  state: string
  action_id?: string
  started_at?: string
  finished_at?: string
  exit_code?: number
  trigger_type?: string
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const diff = (now.getTime() - d.getTime()) / 1000
  if (diff < 60) return `${Math.floor(diff)}秒前`
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`
  return d.toLocaleDateString('zh-CN')
}

export function ExtensionTasks() {
  // E-01-001: silent=true 避免轮询错误时弹出 toast
  const { data: extData, isLoading: extLoading, isError: extError } = useQuery({
    queryKey: ['extensions-dashboard'],
    queryFn: () => apiGet<ExtensionSummary[]>('/api/extensions', undefined, true),
    refetchInterval: 5_000,
  })

  const { data: runsData, isLoading: runsLoading, isError: runsError } = useQuery({
    queryKey: ['extension-runs-dashboard'],
    queryFn: () => apiGet<RunEntry[] | { runs: RunEntry[] } | null>('/api/extensions/runs?include_recent=true', undefined, true),
    refetchInterval: 3_000,
  })

  const extensions = Array.isArray(extData) ? extData : []
  const allRuns: RunEntry[] = Array.isArray(runsData) ? runsData : (runsData && 'runs' in runsData && runsData.runs) ? runsData.runs : []

  const running = allRuns.filter(r => r.state === 'running' || r.state === 'pending')
  const completed = allRuns.filter(r => ['success', 'failed', 'timeout', 'canceled', 'killed'].includes(r.state)).slice(0, 5)
  const scheduled = extensions.filter(e => e.enabled && (e.trigger_type === 'on_schedule'))

  const isLoading = extLoading && runsLoading
  const isError = extError && runsError

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t.dashboard.extensionTasks}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {isError && (
          <div className="flex items-center gap-2 rounded-md border border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-3 py-2 text-sm text-[var(--color-text-error)]">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            <span>扩展任务信息加载失败，将在稍后自动重试。</span>
          </div>
        )}
        {isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        ) : (
        <>
        {/* 运行中 */}
        <div>
          <div className="mb-2 flex items-center gap-2 text-sm font-medium text-[var(--color-text-primary)]">
            <PlayCircle className="h-4 w-4 text-[var(--color-brand-primary)]" />
            {t.dashboard.runningTasks}
          </div>
          {running.length === 0 ? (
            <p className="text-sm text-[var(--color-text-tertiary)]">暂无运行中的任务</p>
          ) : (
            <div className="space-y-1">
              {running.slice(0, 5).map((run) => (
                <Link
                  key={run.run_id}
                  to={`/extensions/${run.extension_name}`}
                  className="flex items-center justify-between rounded border border-[var(--color-border-primary)] px-3 py-2 text-sm hover:bg-[var(--color-surface-hover)]"
                >
                  <span className="font-mono text-[var(--color-text-primary)]">
                    {run.extension_name}
                    {run.action_id ? ` / ${run.action_id}` : ''}
                  </span>
                  <Badge variant="info">运行中</Badge>
                </Link>
              ))}
            </div>
          )}
        </div>

        {/* 最近完成 */}
        <div>
          <div className="mb-2 flex items-center gap-2 text-sm font-medium text-[var(--color-text-primary)]">
            <CheckCircle2 className="h-4 w-4 text-[var(--color-text-success)]" />
            {t.dashboard.recentCompleted}
          </div>
          {completed.length === 0 ? (
            <p className="text-sm text-[var(--color-text-tertiary)]">暂无已完成任务</p>
          ) : (
            <div className="space-y-1">
              {completed.map((run) => (
                <Link
                  key={run.run_id}
                  to={`/extensions/${run.extension_name}`}
                  className="flex items-center justify-between rounded border border-[var(--color-border-primary)] px-3 py-2 text-sm hover:bg-[var(--color-surface-hover)]"
                >
                  <span className="font-mono text-[var(--color-text-primary)]">
                    {run.extension_name}
                    {run.action_id ? ` / ${run.action_id}` : ''}
                  </span>
                  <div className="flex items-center gap-2">
                    {run.finished_at && (
                      <span className="text-xs text-[var(--color-text-tertiary)]">{formatTime(run.finished_at)}</span>
                    )}
                    <Badge variant={run.state === 'success' ? 'success' : 'danger'}>
                      {run.state === 'success' ? '成功' : run.state === 'failed' ? '失败' : run.state === 'timeout' ? '超时' : run.state}
                    </Badge>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </div>

        {/* 定时任务 */}
        <div>
          <div className="mb-2 flex items-center gap-2 text-sm font-medium text-[var(--color-text-primary)]">
            <Clock className="h-4 w-4 text-[var(--color-accent-warning)]" />
            {t.dashboard.nextSchedule}
          </div>
          {scheduled.length === 0 ? (
            <p className="text-sm text-[var(--color-text-tertiary)]">暂无定时任务</p>
          ) : (
            <div className="space-y-1">
              {scheduled.slice(0, 5).map((ext) => (
                <Link
                  key={ext.name}
                  to={`/extensions/${ext.name}`}
                  className="flex items-center justify-between rounded border border-[var(--color-border-primary)] px-3 py-2 text-sm hover:bg-[var(--color-surface-hover)]"
                >
                  <span className="font-mono text-[var(--color-text-primary)]">{ext.name}</span>
                  <span className="text-xs text-[var(--color-text-tertiary)]">
                    运行 {ext.run_count ?? 0} 次
                  </span>
                </Link>
              ))}
            </div>
          )}
        </div>
        </>
        )}
      </CardContent>
    </Card>
  )
}
