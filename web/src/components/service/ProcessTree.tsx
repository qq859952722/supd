// REQ-U-005: 进程树 — 进程树+每进程详情，10秒自动刷新
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { t } from '@/lib/i18n'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { Button } from '@/components/ui/Button'
import { RefreshCw, Cpu, MemoryStick, Clock } from 'lucide-react'

interface ProcessInfo {
  pid: number
  ppid: number
  command: string
  cpu_percent: number
  memory_mb: number
  memory_percent?: number
  status?: string
  state?: string
  thread_count?: number
  started_at?: number
}

interface ProcessesResponse {
  processes: ProcessInfo[]
}

// 将 Linux 进程状态码翻译为用户友好文字
// Linux 进程状态：R=running, S=interruptible sleep, D=uninterruptible sleep,
// Z=zombie, T=stopped, t=tracing stop, X=dead, x=dead, K=wakekill, W=waking
function translateStatus(state: string): { text: string; variant: 'success' | 'warning' | 'danger' | 'secondary' } {
  if (!state) return { text: '-', variant: 'secondary' }
  const lower = state.toLowerCase()
  switch (lower) {
    case 'r':
    case 'running':
      return { text: '运行中', variant: 'success' }
    case 's':
    case 'sleep':
    case 'sleeping':
      // 可中断睡眠是正常状态（等待事件/IO），不等于卡死
      return { text: '运行中', variant: 'success' }
    case 'd':
    case 'disk':
    case 'disk sleep':
      // 不可中断睡眠（通常是IO等待）
      return { text: 'IO等待', variant: 'warning' }
    case 'z':
    case 'zombie':
      return { text: '僵尸', variant: 'danger' }
    case 't':
    case 'stopped':
      return { text: '已停止', variant: 'warning' }
    case 'x':
    case 'dead':
      return { text: '已退出', variant: 'secondary' }
    default:
      return { text: state, variant: 'secondary' }
  }
}

function formatBytes(mb: number): string {
  if (mb < 1024) return `${mb.toFixed(1)}MB`
  return `${(mb / 1024).toFixed(2)}GB`
}

export function ProcessTree({ serviceName }: { serviceName: string }) {
  const [isAutoRefresh, setIsAutoRefresh] = useState(true)

  const { data, refetch, isLoading } = useQuery({
    queryKey: ['service-processes', serviceName],
    queryFn: () => apiGet<ProcessesResponse>(`/api/services/${encodeURIComponent(serviceName)}/processes`),
    refetchInterval: isAutoRefresh ? 10_000 : false,
    enabled: isAutoRefresh,
  })

  const processes = data?.processes ?? []

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle>{t.service.processTree}</CardTitle>
          <div className="flex items-center gap-2">
            <Button
              variant={isAutoRefresh ? 'primary' : 'default'}
              size="sm"
              onClick={() => setIsAutoRefresh(!isAutoRefresh)}
            >
              <RefreshCw className={`h-3.5 w-3.5 ${isLoading ? 'animate-spin' : ''}`} />
              {t.service.autoRefresh}
            </Button>
            {!isAutoRefresh && (
              <Button variant="default" size="sm" onClick={() => refetch()}>
                <RefreshCw className="h-3.5 w-3.5" />
                刷新
              </Button>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {processes.length === 0 ? (
          <p className="text-center text-[var(--color-text-tertiary)] py-8">{t.common.noData}</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>PID</TableHead>
                <TableHead>PPID</TableHead>
                <TableHead>命令</TableHead>
                <TableHead>状态</TableHead>
                <TableHead><Cpu className="h-3.5 w-3.5 inline" /> CPU</TableHead>
                <TableHead><MemoryStick className="h-3.5 w-3.5 inline" /> 内存</TableHead>
                <TableHead><Clock className="h-3.5 w-3.5 inline" /> 启动时间</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {processes.map((proc) => {
                const stateVal = proc.status || proc.state || ''
                const statusInfo = translateStatus(stateVal)
                // started_at 为毫秒时间戳（与 gopsutil CreateTime 一致）
                const startTime = proc.started_at ? new Date(proc.started_at) : null
                return (
                  <TableRow key={proc.pid}>
                    <TableCell className="font-mono text-sm">{proc.pid}</TableCell>
                    <TableCell className="font-mono text-sm text-[var(--color-text-tertiary)]">{proc.ppid}</TableCell>
                    <TableCell className="font-mono text-sm max-w-[200px] truncate" title={proc.command}>
                      {proc.command.split('/').pop() || proc.command}
                    </TableCell>
                    <TableCell>
                      <Badge variant={statusInfo.variant}>
                        {statusInfo.text}
                      </Badge>
                    </TableCell>
                    <TableCell>{proc.cpu_percent.toFixed(1)}%</TableCell>
                    <TableCell>
                      <span>{formatBytes(proc.memory_mb)}</span>
                      {proc.memory_percent != null && (
                        <span className="ml-1 text-[var(--color-text-tertiary)]">({proc.memory_percent.toFixed(1)}%)</span>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-[var(--color-text-tertiary)]">
                      {startTime ? startTime.toLocaleString('zh-CN') : '-'}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
