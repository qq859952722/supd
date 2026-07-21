// 非阻塞式扩展运行通知 — 右下角浮动卡片，不独占窗口
// 支持实时进度显示和最终结果反馈
// 异步模式：POST 立即返回 run_id + state=running，轮询 runs API 获取进度
//
// E-08-001 职责分工说明：
// TaskToast = 事件驱动的瞬时通知。仅在用户主动触发 runExtension 时弹出，
// 显示该次运行的启动/进度/完成状态，完成后 5 秒自动消失。不维护任务列表。
// 运行中任务的持久视图由 BottomDrawer 负责（轮询 /api/extensions/runs）。

import { createContext, useContext, useState, useCallback, useEffect, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiGet, apiPost } from '@/lib/api-client'
import { X, Loader2, CheckCircle2, XCircle, AlertTriangle } from 'lucide-react'

// 终态判断
const TERMINAL_STATES = new Set(['success', 'failed', 'timeout', 'canceled', 'killed'])

interface TaskNotification {
  id: string
  extensionName: string
  action?: string
  serviceName?: string
  runId?: string
  // dryRun: 试运行模式（不产生实际副作用），toast 标题前缀 [试运行]
  dryRun?: boolean
  // pending: POST 进行中; running: POST 返回 state=running, 轮询中; done: 终态
  status: 'pending' | 'running' | 'done'
  resultState?: string
  resultMsg?: string
  resultLevel?: string
  completedAt?: number
}

interface TaskToastContextValue {
  runExtension: (params: {
    extensionName: string
    action?: string
    serviceName?: string
    dryRun?: boolean
  }) => void
}

const TaskToastContext = createContext<TaskToastContextValue | null>(null)

export function useTaskToast() {
  const ctx = useContext(TaskToastContext)
  if (!ctx) {
    return { runExtension: () => {} }
  }
  return ctx
}

let notificationIdCounter = 0

export function TaskToastProvider({ children }: { children: ReactNode }) {
  const [notifications, setNotifications] = useState<TaskNotification[]>([])

  const removeNotification = useCallback((id: string) => {
    setNotifications((prev) => prev.filter((n) => n.id !== id))
  }, [])

  // 子组件检测到终态时回调
  const handleRunComplete = useCallback((id: string, state: string, msg: string, level: string) => {
    setNotifications((prev) =>
      prev.map((n) =>
        n.id === id
          ? {
              ...n,
              status: 'done',
              resultState: state,
              resultMsg: msg,
              resultLevel: level,
              completedAt: Date.now(),
            }
          : n,
      ),
    )
    // 5 秒后自动移除
    setTimeout(() => removeNotification(id), 5000)
  }, [removeNotification])

  const runExtension = useCallback(
    (params: { extensionName: string; action?: string; serviceName?: string; dryRun?: boolean }) => {
      const id = `task-${++notificationIdCounter}`
      const { extensionName, action, serviceName, dryRun } = params

      // §2.2.16: dry_run=true 查询参数表示试运行模式（不产生实际副作用）
      const query = dryRun ? '?dry_run=true' : ''
      const url = serviceName
        ? `/api/services/${encodeURIComponent(serviceName)}/extensions/${encodeURIComponent(extensionName)}/run${query}`
        : `/api/extensions/${encodeURIComponent(extensionName)}/run${query}`

      const runPromise = apiPost(url, action ? { action } : {})

      setNotifications((prev) => [
        ...prev,
        {
          id,
          extensionName,
          action,
          serviceName,
          dryRun,
          status: 'pending',
        },
      ])

      runPromise
        .then((result: any) => {
          // 异步模式：POST 立即返回 state=running + run_id
          if (result?.state === 'running' || result?.state === 'pending') {
            setNotifications((prev) =>
              prev.map((n) =>
                n.id === id
                  ? { ...n, status: 'running', runId: result.run_id }
                  : n,
              ),
            )
          } else {
            // 同步模式或已终态：直接标记完成
            const state = result?.state || 'success'
            setNotifications((prev) =>
              prev.map((n) =>
                n.id === id
                  ? {
                      ...n,
                      status: 'done',
                      runId: result?.run_id,
                      resultState: state,
                      resultMsg: result?.result_msg || '',
                      resultLevel: result?.result_level,
                      completedAt: Date.now(),
                    }
                  : n,
              ),
            )
            setTimeout(() => removeNotification(id), 5000)
          }
        })
        .catch((err) => {
          setNotifications((prev) =>
            prev.map((n) =>
              n.id === id
                ? {
                    ...n,
                    status: 'done',
                    resultState: 'error',
                    resultMsg: err?.message || '运行失败',
                    completedAt: Date.now(),
                  }
                : n,
            ),
          )
          setTimeout(() => removeNotification(id), 5000)
        })
    },
    [removeNotification],
  )

  return (
    <TaskToastContext.Provider value={{ runExtension }}>
      {children}
      <TaskToastPanel
        notifications={notifications}
        onClose={removeNotification}
        onComplete={handleRunComplete}
      />
    </TaskToastContext.Provider>
  )
}

function TaskToastPanel({
  notifications,
  onClose,
  onComplete,
}: {
  notifications: TaskNotification[]
  onClose: (id: string) => void
  onComplete: (id: string, state: string, msg: string, level: string) => void
}) {
  if (notifications.length === 0) return null

  return (
    <div className="fixed bottom-4 right-4 z-[100] flex flex-col gap-2 pointer-events-none">
      {notifications.map((notif) => (
        <TaskToastCard
          key={notif.id}
          notification={notif}
          onClose={() => onClose(notif.id)}
          onComplete={onComplete}
        />
      ))}
    </div>
  )
}

function TaskToastCard({
  notification,
  onClose,
  onComplete,
}: {
  notification: TaskNotification
  onClose: () => void
  onComplete: (id: string, state: string, msg: string, level: string) => void
}) {
  const { id, extensionName, action, serviceName, dryRun, status, runId, resultState, resultMsg, resultLevel } = notification

  // running 状态时轮询特定 run_id 获取进度
  const { data: runData } = useQuery({
    queryKey: ['task-run', runId],
    queryFn: () => apiGet<any>(`/api/extensions/runs/${runId}`),
    enabled: status === 'running' && !!runId,
    refetchInterval: (query) => {
      const data = query.state.data
      if (data && TERMINAL_STATES.has(data.state)) {
        return false
      }
      return 1500
    },
  })

  // 检测终态，回调更新通知
  useEffect(() => {
    if (runData && TERMINAL_STATES.has(runData.state) && status === 'running') {
      onComplete(id, runData.state, runData.result_msg || '', runData.result_level || '')
    }
  }, [runData, status, id, onComplete])

  const isRunning = status === 'pending' || status === 'running'
  const isDone = status === 'done'
  const isSuccess = resultState === 'success'
  const isFailed = resultState === 'failed' || resultState === 'error' || resultState === 'killed' || resultState === 'timeout'
  const isWarning = resultLevel === 'warning' || resultState === 'warning'

  const progress = isRunning ? (runData?.progress ?? 0) : isDone ? 100 : 0
  const displayMsg = isDone
    ? resultMsg
    : runData?.result_msg || '正在执行...'

  return (
    <div className="pointer-events-auto w-80 rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-elevated,var(--color-surface-secondary))] shadow-lg overflow-hidden">
      {/* 头部 */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-[var(--color-border-secondary)]">
        <div className="flex items-center gap-2 min-w-0">
          {isRunning && <Loader2 className="h-3.5 w-3.5 animate-spin text-[var(--color-brand-primary)] shrink-0" />}
          {isDone && isSuccess && !isWarning && <CheckCircle2 className="h-3.5 w-3.5 text-[var(--color-text-success)] shrink-0" />}
          {isDone && isFailed && <XCircle className="h-3.5 w-3.5 text-[var(--color-text-error)] shrink-0" />}
          {isDone && isWarning && <AlertTriangle className="h-3.5 w-3.5 text-[var(--color-text-warning)] shrink-0" />}
          <span className="text-xs font-medium text-[var(--color-text-primary)] truncate">
            {dryRun && <span className="text-[var(--color-brand-primary)]">[试运行] </span>}
            {extensionName}
            {action ? ` · ${action}` : ''}
          </span>
        </div>
        <button
          onClick={onClose}
          className="shrink-0 rounded p-0.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
        >
          <X className="h-3 w-3" />
        </button>
      </div>

      {/* 进度条（运行中时显示） */}
      {isRunning && (
        <div className="px-3 py-1.5">
          <div className="flex items-center justify-between text-[10px] text-[var(--color-text-tertiary)] mb-1">
            <span>{serviceName ? `${serviceName} · ` : ''}执行中</span>
            <span className="font-mono">{progress}%</span>
          </div>
          <div className="h-1 w-full rounded-full bg-[var(--color-surface-tertiary)] overflow-hidden">
            <div
              className="h-full rounded-full bg-[var(--color-brand-primary)] transition-all duration-300"
              style={{ width: `${Math.min(progress, 100)}%` }}
            />
          </div>
        </div>
      )}

      {/* 结果消息 */}
      {displayMsg && (
        <div className="px-3 py-2 text-xs text-[var(--color-text-secondary)] break-all">
          {displayMsg}
        </div>
      )}

      {/* 完成状态徽章 */}
      {isDone && (
        <div className="px-3 py-1.5 border-t border-[var(--color-border-secondary)] flex items-center justify-between">
          <span
            className={`text-[10px] font-medium ${
              isSuccess && !isWarning
                ? 'text-[var(--color-text-success)]'
                : isFailed
                  ? 'text-[var(--color-text-error)]'
                  : 'text-[var(--color-text-warning)]'
            }`}
          >
            {isSuccess && !isWarning ? '✓ 成功' : isFailed ? '✗ 失败' : '⚠ 告警'}
          </span>
          <span className="text-[10px] text-[var(--color-text-tertiary)]">
            {resultState}
          </span>
        </div>
      )}
    </div>
  )
}
