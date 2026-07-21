// REQ-U-007: 定时任务页面
// 表格展示所有 on_schedule 扩展 + 立即执行 + 实时执行反馈抽屉

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost, apiLongPoll } from '@/lib/api-client'
import { Badge } from '@/components/ui/Badge'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { Button } from '@/components/ui/Button'
import { SkeletonTable } from '@/components/ui/Skeleton'
import { toast } from '@/components/ui/Toast'
import { t } from '@/lib/i18n'
import { RefreshCw, Play, X, Loader2, CheckCircle, XCircle, AlertTriangle, AlertCircle, Ban, FileText, ChevronRight, ChevronDown, Settings, Clock } from 'lucide-react'
import { useState, useRef, useEffect, useCallback, Fragment } from 'react'
import { useNavigate } from 'react-router'
import { getErrorMessage } from '@/lib/error-utils'
import type { ExtSummary, RunResult } from '@/types/extension'

// CronEntryInfo — GET /api/cron 返回结构
interface CronEntry {
  extension_name: string
  action_id: string
  schedule: string
  next_run?: string
  service?: string
}

// I-03-001 修复：ExtSummary / RunResult 抽取到 @/types/extension

// 日志长轮询响应
interface LogsResponse {
  lines: string[]
  next_pos: number
  has_more: boolean
}

// 执行反馈抽屉状态
interface RunFeedback {
  extensionName: string
  actionId: string
  runId: string | null
  status: 'running' | 'done' | 'error'
  result: RunResult | null
  logs: string[]
  progress: number
  progressMsg: string
  resultLevel: string
  resultMsg: string
}

// 终态判断 — 与 TaskToast.tsx 一致（success/failed/timeout/canceled/killed）
const TERMINAL_STATES = new Set(['success', 'failed', 'timeout', 'canceled', 'killed'])

// 解析 ::progress:: N "msg" 协议
function parseProgress(line: string): { percent: number; msg: string } | null {
  const match = line.match(/^::progress::\s+(\d+)\s+"([^"]*)"$/)
  if (match && match[1] !== undefined && match[2] !== undefined) {
    return { percent: parseInt(match[1], 10), msg: match[2] }
  }
  return null
}

// 解析 ::result:: success|error|warning "msg" 协议
function parseResult(line: string): { level: string; msg: string } | null {
  const match = line.match(/^::result::\s+(success|error|warning)\s+"([^"]*)"$/)
  if (match && match[1] !== undefined && match[2] !== undefined) {
    return { level: match[1], msg: match[2] }
  }
  return null
}

// 状态图标 — F-04-001 修复：区分 killed/canceled，与 BottomDrawer 一致
function statusIcon(state: string) {
  switch (state) {
    case 'running':
      return <Loader2 className="h-4 w-4 animate-spin text-[var(--color-brand-primary)]" />
    case 'success':
      return <CheckCircle className="h-4 w-4 text-[var(--color-text-success)]" />
    case 'failed':
      return <XCircle className="h-4 w-4 text-[var(--color-text-error)]" />
    case 'killed':
      // F-04-002: killed = 系统强制杀死（达到 max_retries 或 SIGKILL），用 AlertCircle + 警告色与 failed 区分
      return <AlertCircle className="h-4 w-4 text-[var(--color-accent-warning)]" />
    case 'timeout':
      // timeout = 系统超时，警告色
      return <AlertTriangle className="h-4 w-4 text-[var(--color-text-warning)]" />
    case 'canceled':
      // canceled = 用户主动取消，中性色，与 timeout 区分
      return <Ban className="h-4 w-4 text-[var(--color-text-tertiary)]" />
    case 'pending':
      // F-04-001 修复：pending = 已创建未开始，用 Clock 中性色
      return <Clock className="h-4 w-4 text-[var(--color-text-tertiary)]" />
    default:
      return null
  }
}

// 执行反馈抽屉组件
function RunFeedbackDrawer({
  feedback,
  onClose,
}: {
  feedback: RunFeedback
  onClose: () => void
}) {
  const logEndRef = useRef<HTMLDivElement>(null)

  // 自动滚动到日志底部
  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [feedback.logs])

  // 计算耗时
  let durationStr = '-'
  if (feedback.result?.started_at && feedback.result?.finished_at) {
    const start = new Date(feedback.result.started_at).getTime()
    const end = new Date(feedback.result.finished_at).getTime()
    if (!isNaN(start) && !isNaN(end) && end >= start) {
      const ms = end - start
      if (ms < 1000) durationStr = `${ms}ms`
      else durationStr = `${(ms / 1000).toFixed(1)}s`
    }
  }

  const displayProgress = feedback.result?.progress ?? feedback.progress
  const displayResultLevel = feedback.result?.result_level ?? feedback.resultLevel
  const displayResultMsg = feedback.result?.result_msg ?? feedback.resultMsg

  return (
    <>
      {/* 遮罩 */}
      <div
        className="fixed inset-0 z-50 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
      />
      {/* 抽屉面板（右侧滑出） */}
      <div className="fixed right-0 top-0 z-[51] h-full w-full max-w-xl bg-[var(--color-surface-secondary)] shadow-[var(--shadow-lg)] flex flex-col animate-in slide-in-from-right">
        {/* 头部 */}
        <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-5 py-4">
          <div className="flex items-center gap-2">
            <h2 className="text-base font-semibold text-[var(--color-text-primary)]">
              执行反馈
            </h2>
            <Badge variant="secondary" className="font-mono text-xs">
              {feedback.extensionName}
            </Badge>
            {feedback.actionId && (
              <Badge variant="default" className="font-mono text-xs">
                {feedback.actionId}
              </Badge>
            )}
          </div>
          <button
            onClick={onClose}
            className="rounded-sm p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* 内容区 */}
        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          {/* 状态行 */}
          <div className="flex items-center gap-2">
            {feedback.status === 'running' ? (
              <>
                <Loader2 className="h-5 w-5 animate-spin text-[var(--color-brand-primary)]" />
                <span className="text-sm font-medium text-[var(--color-text-primary)]">执行中...</span>
              </>
            ) : feedback.result ? (
              <>
                {statusIcon(feedback.result.state)}
                <span className="text-sm font-medium text-[var(--color-text-primary)]">
                  {feedback.result.state}
                </span>
                <Badge
                  variant={
                    feedback.result.state === 'success'
                      ? 'success'
                      : feedback.result.state === 'failed'
                        ? 'danger'
                        : feedback.result.state === 'killed'
                          ? 'warning'
                          : 'warning'
                  }
                >
                  {feedback.result.state}
                </Badge>
              </>
            ) : (
              <>
                <XCircle className="h-5 w-5 text-[var(--color-text-error)]" />
                <span className="text-sm font-medium text-[var(--color-text-error)]">执行失败</span>
              </>
            )}
          </div>

          {/* 进度条 */}
          {(displayProgress > 0 || feedback.status === 'running') && (
            <div className="space-y-1.5">
              <div className="flex items-center justify-between text-xs text-[var(--color-text-secondary)]">
                <span>进度</span>
                <span className="font-mono">{displayProgress}%</span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-[var(--color-surface-tertiary)]">
                <div
                  className="h-full rounded-full bg-[var(--color-brand-primary)] transition-all duration-300"
                  style={{ width: `${displayProgress}%` }}
                />
              </div>
              {feedback.progressMsg && (
                <p className="text-xs text-[var(--color-text-tertiary)]">{feedback.progressMsg}</p>
              )}
            </div>
          )}

          {/* 最终结果 */}
          {displayResultLevel && (
            <div
              className={`rounded-md border p-3 text-sm ${
                displayResultLevel === 'success'
                  ? 'border-[var(--color-border-secondary)] bg-[var(--color-surface-success)] text-[var(--color-text-success)]'
                  : displayResultLevel === 'error'
                    ? 'border-[var(--color-border-error)] bg-[var(--color-surface-error)] text-[var(--color-text-error)]'
                    : 'border-[var(--color-border-secondary)] bg-[var(--color-surface-warning)] text-[var(--color-text-warning)]'
              }`}
            >
              <span className="font-medium">::result:: {displayResultLevel}</span>
              {displayResultMsg && <span className="ml-2">"{displayResultMsg}"</span>}
            </div>
          )}

          {/* 退出码和耗时 */}
          {feedback.result && (
            <div className="grid grid-cols-2 gap-3">
              <div className="rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] p-3">
                <div className="text-xs text-[var(--color-text-tertiary)]">退出码</div>
                <div className="mt-0.5 font-mono text-lg font-semibold text-[var(--color-text-primary)]">
                  {feedback.result.exit_code}
                </div>
              </div>
              <div className="rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] p-3">
                <div className="text-xs text-[var(--color-text-tertiary)]">耗时</div>
                <div className="mt-0.5 font-mono text-lg font-semibold text-[var(--color-text-primary)]">
                  {durationStr}
                </div>
              </div>
            </div>
          )}

          {/* 实时日志输出 */}
          <div className="space-y-1.5">
            <div className="text-xs font-medium text-[var(--color-text-secondary)]">日志输出</div>
            <div className="h-64 overflow-y-auto rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] p-3">
              {feedback.logs.length > 0 ? (
                <pre className="whitespace-pre-wrap break-all text-xs font-mono text-[var(--color-text-secondary)] leading-relaxed">
                  {feedback.logs.map((line, i) => {
                    const prog = parseProgress(line)
                    const res = parseResult(line)
                    if (prog) {
                      return (
                        <div key={i} className="text-[var(--color-brand-primary)]">
                          {`[progress] ${prog.percent}% — ${prog.msg}`}
                        </div>
                      )
                    }
                    if (res) {
                      return (
                        <div
                          key={i}
                          className={
                            res.level === 'success'
                              ? 'text-[var(--color-text-success)]'
                              : res.level === 'error'
                                ? 'text-[var(--color-text-error)]'
                                : 'text-[var(--color-text-warning)]'
                          }
                        >
                          {`[result] ${res.level} — ${res.msg}`}
                        </div>
                      )
                    }
                    return <div key={i}>{line}</div>
                  })}
                </pre>
              ) : (
                <div className="flex h-full items-center justify-center text-xs text-[var(--color-text-tertiary)]">
                  {feedback.status === 'running' ? '等待输出...' : '暂无日志'}
                </div>
              )}
              <div ref={logEndRef} />
            </div>
          </div>
        </div>

        {/* 底部操作 */}
        <div className="flex justify-end gap-2 border-t border-[var(--color-border-primary)] px-5 py-3">
          <Button variant="default" size="sm" onClick={onClose}>
            关闭
          </Button>
        </div>
      </div>
    </>
  )
}

export default function CronTasksPage() {
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState<RunFeedback | null>(null)
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null)
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set())
  const pollControllerRef = useRef<AbortController | null>(null)
  const navigate = useNavigate()
  // F-05-002: 浏览器本地时区，用于历史记录表头提示
  const localTz = Intl.DateTimeFormat().resolvedOptions().timeZone

  // 获取定时任务条目
  // E-01-001 修复：添加 isError 处理，请求失败时显示错误提示而非静默忽略
  const { data: cronData, isLoading, isError } = useQuery({
    queryKey: ['cron-extensions'],
    queryFn: () => apiGet<CronEntry[] | null>('/api/cron'),
  })

  // 获取扩展列表（用于补充 last_run_at / last_status）
  const { data: extData } = useQuery({
    queryKey: ['extensions-for-cron'],
    queryFn: () => apiGet<ExtSummary[] | null>('/api/extensions'),
  })

  // REQ-D-004: 获取定时任务运行历史
  const { data: historyData } = useQuery({
    queryKey: ['cron-history'],
    queryFn: () => apiGet<RunResult[] | null>('/api/cron/history'),
  })

  const cronItems = Array.isArray(cronData) ? cronData : []
  const extMap = new Map<string, ExtSummary>()
  if (Array.isArray(extData)) {
    extData.forEach((e) => extMap.set(e.name, e))
  }

  // 轮询日志的清理函数
  useEffect(() => {
    return () => {
      pollControllerRef.current?.abort()
    }
  }, [])

  // 轮询获取日志
  // F-05-001 修复：异步模式下 POST 立即返回 state=running，需在此轮询 /runs/<id> 获取终态
  const pollLogs = useCallback(async (runId: string) => {
    let sincePos = 0
    pollControllerRef.current?.abort()
    const controller = new AbortController()
    pollControllerRef.current = controller
    let terminated = false

    try {
      // 循环获取日志直到任务终态或抽屉关闭
      for (let i = 0; i < 200 && !terminated; i++) {
        if (controller.signal.aborted) break

        // 先获取 run 状态：终态时更新抽屉的 status/result，并标记退出
        try {
          const run = await apiGet<RunResult>(
            `/api/extensions/runs/${encodeURIComponent(runId)}`,
          )
          if (run && TERMINAL_STATES.has(run.state)) {
            setFeedback((prev) =>
              prev
                ? {
                    ...prev,
                    status: 'done',
                    result: run,
                    progress: run.progress || prev.progress,
                    resultLevel: run.result_level || prev.resultLevel,
                    resultMsg: run.result_msg || prev.resultMsg,
                  }
                : prev,
            )
            terminated = true
            // 终态后仍执行下方日志获取一次，确保拿到最后一批日志
          } else if (run) {
            // 运行中：同步进度（::progress:: 协议解析为权威来源，此处仅作兜底）
            setFeedback((prev) =>
              prev
                ? {
                    ...prev,
                    progress: run.progress || prev.progress,
                  }
                : prev,
            )
          }
        } catch {
          // 状态获取失败，不阻塞日志轮询
        }

        const resp = await apiLongPoll<LogsResponse>(
          `/api/extensions/runs/${encodeURIComponent(runId)}/logs`,
          { since_pos: sincePos, wait: 'true' },
          controller.signal,
        )

        if (resp.lines && resp.lines.length > 0) {
          setFeedback((prev) => {
            if (!prev) return prev
            const newLogs = [...prev.logs, ...resp.lines]
            // 解析最新的 progress 和 result
            let progress = prev.progress
            let progressMsg = prev.progressMsg
            let resultLevel = prev.resultLevel
            let resultMsg = prev.resultMsg
            for (const line of resp.lines) {
              const prog = parseProgress(line)
              if (prog) {
                progress = prog.percent
                progressMsg = prog.msg
              }
              const res = parseResult(line)
              if (res) {
                resultLevel = res.level
                resultMsg = res.msg
              }
            }
            return { ...prev, logs: newLogs, progress, progressMsg, resultLevel, resultMsg }
          })
        }

        sincePos = resp.next_pos
        if (!resp.has_more && resp.lines.length === 0) {
          // 没有更多日志，退出循环
          break
        }
        if (terminated) {
          // 已终态且已拿到最后一批日志，退出
          break
        }
        // 短暂等待后继续轮询
        await new Promise((r) => setTimeout(r, 500))
      }
    } catch {
      // 轮询被中断或出错，静默处理
    }
  }, [])

  // 立即执行
  const runNowMutation = useMutation({
    mutationFn: async ({ name, action }: { name: string; action: string }) => {
      // 打开抽屉，显示执行中状态
      setFeedback({
        extensionName: name,
        actionId: action,
        runId: null,
        status: 'running',
        result: null,
        logs: [],
        progress: 0,
        progressMsg: '',
        resultLevel: '',
        resultMsg: '',
      })

      // F-05-001 修复：异步模式 — POST 立即返回 state=running + run_id
      // E-02 修复：silent=true 避免与 onError 重复 toast
      const result = await apiPost<RunResult>(
        `/api/extensions/${encodeURIComponent(name)}/run`,
        action ? { action } : {},
        true,
      )
      return result
    },
    onSuccess: (result) => {
      // F-05-001 修复：根据返回 state 判断是异步运行中还是已终态（dry_run 等场景）
      const isTerminal = TERMINAL_STATES.has(result.state)
      setFeedback((prev) =>
        prev
          ? {
              ...prev,
              runId: result.run_id,
              status: isTerminal ? 'done' : 'running',
              result,
              progress: result.progress || prev.progress,
              resultLevel: result.result_level || prev.resultLevel,
              resultMsg: result.result_msg || prev.resultMsg,
            }
          : prev,
      )

      // 启动日志 + 状态轮询（终态时 pollLogs 内部会拉取剩余日志后退出）
      if (result.run_id) {
        pollLogs(result.run_id)
      }

      // toast：异步模式提示"已触发"，已终态提示"执行完成"
      if (isTerminal) {
        toast.success(`${result.extension_name} 执行完成: ${result.state}`)
      } else {
        toast.success(`${result.extension_name} 已触发，执行中...`)
      }
      queryClient.invalidateQueries({ queryKey: ['cron-extensions'] })
      queryClient.invalidateQueries({ queryKey: ['extensions-for-cron'] })
      queryClient.invalidateQueries({ queryKey: ['cron-history'] })
    },
    onError: (err: unknown, vars) => {
      setFeedback((prev) =>
        prev
          ? {
              ...prev,
              status: 'error',
            }
          : prev,
      )
      const msg = getErrorMessage(err, `触发 ${vars.name} 失败`)
      toast.error(msg)
    },
  })

  const handleRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ['cron-extensions'] })
    queryClient.invalidateQueries({ queryKey: ['extensions-for-cron'] })
    queryClient.invalidateQueries({ queryKey: ['cron-history'] })
  }

  const toggleExpand = (rowKey: string) => {
    setExpandedRows((prev) => {
      const next = new Set(prev)
      if (next.has(rowKey)) {
        next.delete(rowKey)
      } else {
        next.add(rowKey)
      }
      return next
    })
  }

  const handleCloseFeedback = () => {
    pollControllerRef.current?.abort()
    setFeedback(null)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">{t.cron.title}</h1>
        <Button variant="default" size="sm" onClick={handleRefresh}>
          <RefreshCw className="h-4 w-4" />
          {t.common.refresh}
        </Button>
      </div>

      {/* F-05-002 修复：明确标注 cron 调度使用的时区（规格 §2.4/§2.8.1 强制 Asia/Shanghai） */}
      <div className="rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-tertiary)] px-3 py-1.5 text-xs text-[var(--color-text-secondary)]">
        ⏰ Cron 调度时区：<span className="font-mono font-medium text-[var(--color-text-primary)]">Asia/Shanghai</span>（服务器时区）
        <span className="ml-2 text-[var(--color-text-tertiary)]">下次执行时间显示为浏览器本地时区（{localTz}）</span>
      </div>

      {/* 定时任务表格 */}
      {isLoading ? (
        <SkeletonTable rows={5} cols={7} />
      ) : isError ? (
        <div className="rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-4 py-3 text-sm text-[var(--color-text-error)]">
          加载定时任务失败，请点击右上角刷新按钮重试
        </div>
      ) : cronItems.length > 0 ? (
        <div className="rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)]">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t.cron.colName}</TableHead>
                <TableHead>{t.cron.colSchedule}</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>{t.cron.colNextRun}</TableHead>
                <TableHead>{t.cron.colLastRun}</TableHead>
                <TableHead>{t.cron.colStatus}</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {cronItems.map((item) => {
                const ext = extMap.get(item.extension_name)
                const lastRunAt = ext?.last_run_at
                const lastStatus = ext?.last_status
                const rowKey = `${item.extension_name}-${item.action_id}-${item.schedule}`
                const isExpanded = expandedRows.has(rowKey)
                const taskHistory = (historyData ?? []).filter(
                  (r) => r.extension_name === item.extension_name && (!item.action_id || r.action_id === item.action_id),
                )
                return (
                  <Fragment key={rowKey}>
                    <TableRow>
                      <TableCell className="font-medium">
                        <div className="flex items-center gap-1.5">
                          <button
                            onClick={() => toggleExpand(rowKey)}
                            className="rounded p-0.5 hover:bg-[var(--color-surface-hover)] text-[var(--color-text-tertiary)]"
                            title={isExpanded ? '收起历史' : '展开历史'}
                          >
                            {isExpanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                          </button>
                          <button
                            onClick={() => navigate(`/extensions/${item.extension_name}`)}
                            className="hover:text-[var(--color-brand-primary)] hover:underline cursor-pointer"
                            title="跳转到扩展详情配置"
                          >
                            {item.extension_name}
                          </button>
                        </div>
                      </TableCell>
                      <TableCell className="font-mono text-xs">{item.schedule}</TableCell>
                      <TableCell className="font-mono text-xs text-[var(--color-text-secondary)]">
                        {item.action_id || '-'}
                      </TableCell>
                      <TableCell className="text-xs whitespace-nowrap text-[var(--color-text-secondary)]" title={item.next_run ? `${item.next_run} (RFC3339 原始值，显示转换为本地时区)` : ''}>
                        {item.next_run ? new Date(item.next_run).toLocaleString('zh-CN') : '-'}
                      </TableCell>
                      <TableCell className="text-xs whitespace-nowrap" title={lastRunAt ? `${lastRunAt} (RFC3339 原始值，显示转换为本地时区)` : ''}>
                        {lastRunAt ? new Date(lastRunAt).toLocaleString('zh-CN') : '-'}
                      </TableCell>
                      <TableCell>
                        {lastStatus ? (
                          <Badge
                            variant={
                              lastStatus === 'success'
                                ? 'success'
                                : lastStatus === 'failed' || lastStatus === 'killed'
                                  ? 'danger'
                                  : lastStatus === 'timeout' || lastStatus === 'canceled'
                                    ? 'warning'
                                    : 'default'
                            }
                          >
                            {lastStatus}
                          </Badge>
                        ) : (
                          <span className="text-xs text-[var(--color-text-tertiary)]">-</span>
                        )}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1.5">
                          <Button
                            variant="primary"
                            size="sm"
                            onClick={() =>
                              runNowMutation.mutate({
                                name: item.extension_name,
                                action: item.action_id,
                              })
                            }
                            disabled={runNowMutation.isPending}
                          >
                            <Play className="h-3.5 w-3.5" />
                            立即执行
                          </Button>
                          <Button
                            variant="default"
                            size="sm"
                            onClick={() => navigate(`/extensions/${item.extension_name}`)}
                            title="跳转到扩展详情配置"
                          >
                            <Settings className="h-3.5 w-3.5" />
                            配置
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                    {isExpanded && (
                      <TableRow className="bg-[var(--color-bg-tertiary)]">
                        <TableCell colSpan={6} className="p-3">
                          <div className="rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-secondary)]">
                            <div className="flex items-center justify-between border-b border-[var(--color-border-secondary)] px-3 py-1.5">
                              <span className="text-xs font-medium text-[var(--color-text-secondary)]">
                                执行历史 ({taskHistory.length})
                              </span>
                            </div>
                            {taskHistory.length > 0 ? (
                              <div className="max-h-60 overflow-auto">
                                <Table>
                                  <TableHeader>
                                    <TableRow>
                                      <TableHead className="text-xs">状态</TableHead>
                                      <TableHead className="text-xs">
                                        启动时间
                                        <div className="text-[10px] font-normal text-[var(--color-text-tertiary)]">时区: {localTz}</div>
                                      </TableHead>
                                      <TableHead className="text-xs">
                                        结束时间
                                        <div className="text-[10px] font-normal text-[var(--color-text-tertiary)]">时区: {localTz}</div>
                                      </TableHead>
                                      <TableHead className="text-xs">退出码</TableHead>
                                      <TableHead className="text-xs">结果</TableHead>
                                      <TableHead className="text-right text-xs">操作</TableHead>
                                    </TableRow>
                                  </TableHeader>
                                  <TableBody>
                                    {taskHistory.slice(0, 20).map((run) => (
                                      <TableRow key={run.run_id}>
                                        <TableCell>
                                          <Badge
                                            variant={
                                              run.state === 'success'
                                                ? 'success'
                                                : run.state === 'failed' || run.state === 'killed'
                                                  ? 'danger'
                                                  : run.state === 'running'
                                                    ? 'info'
                                                    : 'warning'
                                            }
                                          >
                                            {statusIcon(run.state)}
                                            {run.state}
                                          </Badge>
                                        </TableCell>
                                        <TableCell className="text-xs whitespace-nowrap font-mono" title={run.started_at ? `${run.started_at} (RFC3339 原始值，显示转换为本地时区)` : ''}>{run.started_at ? new Date(run.started_at).toLocaleString('zh-CN') : '-'}</TableCell>
                                        <TableCell className="text-xs whitespace-nowrap font-mono" title={run.finished_at ? `${run.finished_at} (RFC3339 原始值，显示转换为本地时区)` : ''}>{run.finished_at ? new Date(run.finished_at).toLocaleString('zh-CN') : '-'}</TableCell>
                                        <TableCell className="text-xs font-mono">{run.exit_code}</TableCell>
                                        <TableCell className="text-xs text-[var(--color-text-secondary)] max-w-xs truncate" title={run.result_msg || ''}>
                                          {run.result_msg || '-'}
                                        </TableCell>
                                        <TableCell className="text-right">
                                          <Button variant="default" size="sm" onClick={() => setSelectedRunId(run.run_id)}>
                                            <FileText className="h-3.5 w-3.5" />
                                            日志
                                          </Button>
                                        </TableCell>
                                      </TableRow>
                                    ))}
                                  </TableBody>
                                </Table>
                              </div>
                            ) : (
                              <div className="py-4 text-center text-xs text-[var(--color-text-tertiary)]">
                                暂无执行历史
                              </div>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                )
              })}
            </TableBody>
          </Table>
        </div>
      ) : (
        <div className="rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] py-8 text-center text-sm text-[var(--color-text-secondary)]">
          {t.cron.empty}
        </div>
      )}

      {/* 执行反馈抽屉 */}
      {feedback && (
        <RunFeedbackDrawer feedback={feedback} onClose={handleCloseFeedback} />
      )}

      {/* 运行日志查看对话框 */}
      {selectedRunId && (
        <CronRunLogDialog runId={selectedRunId} onClose={() => setSelectedRunId(null)} />
      )}
    </div>
  )
}

// 运行日志查看对话框
function CronRunLogDialog({ runId, onClose }: { runId: string; onClose: () => void }) {
  const { data: logsData, isLoading } = useQuery({
    queryKey: ['cron-run-logs', runId],
    queryFn: async () => {
      const resp = await apiGet<{ lines: string[]; next_pos: number; has_more: boolean }>(
        `/api/extensions/runs/${encodeURIComponent(runId)}/logs`,
        { since_pos: 0 },
      )
      return resp
    },
    enabled: !!runId,
  })

  const lines = logsData?.lines ?? []

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative z-10 w-full max-w-3xl max-h-[80vh] flex flex-col rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] shadow-md">
        <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-5 py-3">
          <div className="flex items-center gap-2">
            <FileText className="h-4 w-4 text-[var(--color-text-tertiary)]" />
            <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">运行日志</h3>
            <Badge variant="secondary" className="font-mono text-xs">{runId.slice(0, 8)}</Badge>
          </div>
          <button
            onClick={onClose}
            className="rounded-sm p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="flex-1 overflow-auto p-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
              <Loader2 className="h-4 w-4 animate-spin mr-2" />
              {t.common.loading}
            </div>
          ) : lines.length > 0 ? (
            <pre className="whitespace-pre-wrap break-all text-xs font-mono text-[var(--color-text-secondary)] leading-relaxed">
              {lines.join('\n')}
            </pre>
          ) : (
            <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
              暂无日志
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
