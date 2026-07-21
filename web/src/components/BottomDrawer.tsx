// REQ-U-016: 底部浮窗 — 运行中任务
// 所有页面可见、可展开/收起、进度条+取消按钮、60秒淡出已完成任务
//
// E-08-001 职责分工说明：
// BottomDrawer = 运行中任务的持久视图（唯一任务列表）。轮询 /api/extensions/runs
// 获取所有运行中任务，可展开查看进度、取消任务。已完成任务 60 秒后淡出。
// 瞬时启动/完成通知由 TaskToast 负责（事件驱动，非持久列表）。

import { useState, useEffect, useCallback } from 'react'
import { ChevronUp, X, Loader2, CheckCircle2, AlertCircle, AlertTriangle, Ban, Clock, XCircle } from 'lucide-react'
import { t } from '@/lib/i18n'

interface RunningTask {
  id: string
  extensionName: string
  action: string
  startedAt: number
  progress?: number
  // E-08-001/F-04-001 修复：保留全部7种任务状态语义，不再坍缩为 failed
  status: 'running' | 'pending' | 'success' | 'failed' | 'timeout' | 'canceled' | 'killed'
  completedAt?: number
}

// 最近完成任务60秒淡出 (REQ-U-016)
const FADE_OUT_MS = 60_000

function formatDuration(ms: number): string {
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const remainSeconds = seconds % 60
  return `${minutes}m${remainSeconds}s`
}

function TaskItem({
  task,
  onCancel,
}: {
  task: RunningTask
  onCancel?: (id: string) => void
}) {
  const isRunning = task.status === 'running'
  // F-04-001: pending 视为活跃态，可取消并显示已等待时长
  const isActive = isRunning || task.status === 'pending'
  const elapsed = isActive
    ? Date.now() - task.startedAt
    : task.completedAt
      ? task.completedAt - task.startedAt
      : 0

  return (
    <div className="flex items-center gap-3 rounded-md border border-[var(--color-border-primary)] bg-[var(--color-surface-primary)] px-3 py-2">
      {/* 状态图标 — F-04-002 修复：区分 failed/killed，与 CronTasks 视觉映射一致 */}
      {isRunning && <Loader2 className="h-4 w-4 shrink-0 animate-spin text-[var(--color-brand-primary)]" />}
      {task.status === 'pending' && <Clock className="h-4 w-4 shrink-0 text-[var(--color-text-tertiary)]" />}
      {task.status === 'success' && <CheckCircle2 className="h-4 w-4 shrink-0 text-[var(--color-text-success)]" />}
      {task.status === 'failed' && <XCircle className="h-4 w-4 shrink-0 text-[var(--color-text-error)]" />}
      {task.status === 'killed' && <AlertCircle className="h-4 w-4 shrink-0 text-[var(--color-accent-warning)]" />}
      {task.status === 'timeout' && <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--color-text-warning)]" />}
      {task.status === 'canceled' && <Ban className="h-4 w-4 shrink-0 text-[var(--color-text-tertiary)]" />}

      {/* 信息 */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-[var(--color-text-primary)] truncate">
            {task.extensionName}
          </span>
          <span className="text-xs text-[var(--color-text-tertiary)]">{task.action}</span>
        </div>
        <div className="mt-1 flex items-center gap-2">
          {/* 进度条 */}
          {isRunning && (
            <div className="h-1 flex-1 rounded-full bg-[var(--color-surface-tertiary)]">
              <div
                className="h-full rounded-full bg-[var(--color-brand-primary)] transition-all"
                style={{ width: task.progress != null ? `${task.progress}%` : '0%' }}
              />
            </div>
          )}
          <span className="text-xs text-[var(--color-text-tertiary)] shrink-0">
            {formatDuration(elapsed)}
          </span>
        </div>
      </div>

      {/* 取消按钮 */}
      {isActive && onCancel && (
        <button
          onClick={() => onCancel(task.id)}
          className="shrink-0 rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-error)] hover:bg-[var(--color-surface-error)] transition-colors"
          title={t.drawer.cancel}
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  )
}

interface BottomDrawerProps {
  tasks?: RunningTask[]
  onCancelTask?: (id: string) => void
}

export function BottomDrawer({ tasks = [], onCancelTask }: BottomDrawerProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const [visibleTasks, setVisibleTasks] = useState<RunningTask[]>(tasks)

  // 60秒淡出已完成任务
  useEffect(() => {
    const now = Date.now()
    const active = tasks.filter((task) => {
      // running/pending 视为活跃任务，常驻显示
      if (task.status === 'running' || task.status === 'pending') return true
      if (task.completedAt && now - task.completedAt < FADE_OUT_MS) return true
      return false
    })
    setVisibleTasks(active)
  }, [tasks])

  // 定时清理已淡出任务
  useEffect(() => {
    const interval = setInterval(() => {
      const now = Date.now()
      setVisibleTasks((prev) =>
        prev.filter((task) => {
          if (task.status === 'running' || task.status === 'pending') return true
          if (task.completedAt && now - task.completedAt < FADE_OUT_MS) return true
          return false
        }),
      )
    }, 5000)
    return () => clearInterval(interval)
  }, [])

  // F-04-001 修复：runningCount 包含 running + pending，统一视为运行中
  const runningCount = visibleTasks.filter((t) => t.status === 'running' || t.status === 'pending').length
  const hasVisible = visibleTasks.length > 0

  const handleCancel = useCallback(
    (id: string) => {
      onCancelTask?.(id)
    },
    [onCancelTask],
  )

  if (!hasVisible) return null

  return (
    <div className="fixed bottom-0 left-0 right-0 z-50">
      {isExpanded && (
        <div className="max-h-[50vh] overflow-y-auto border-t border-[var(--color-border-primary)] bg-[var(--color-surface-primary)] p-4">
          <div className="mx-auto max-w-7xl space-y-2">
            {visibleTasks.map((task) => (
              <TaskItem key={task.id} task={task} onCancel={handleCancel} />
            ))}
          </div>
        </div>
      )}
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="flex w-full items-center justify-between border-t border-[var(--color-border-primary)] bg-[var(--color-surface-primary)] px-4 py-2 text-sm text-[var(--color-brand-primary)] hover:bg-[var(--color-surface-secondary)] transition-colors"
      >
        <span>
          {runningCount > 0
            ? `${runningCount} ${t.drawer.tasksRunning}`
            : t.drawer.tasksCompleted}
        </span>
        <ChevronUp className={`h-4 w-4 transition-transform ${isExpanded ? '' : 'rotate-180'}`} />
      </button>
    </div>
  )
}
