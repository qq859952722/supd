// 节点 09-4：操作中心页（§九 / §十二.5.6 / v5 同页两区）。
// 上区：操作卡片区（触发 + 上次执行摘要）；下区：执行历史列表 + 右侧详情抽屉。
// 卡片字段与 GET /api/operations 真实 JSON 对齐；时间戳为 epoch 毫秒 number。

import { useState, useCallback, useEffect, Fragment } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router'
import {
  PlaySquare, Settings2, Loader2, CheckCircle, XCircle, AlertTriangle,
  ChevronRight, ChevronDown, FileText, ExternalLink, Inbox, AlertOctagon,
  Trash2, Plus, Puzzle, SlidersHorizontal,
} from 'lucide-react'
import {
  getOperations, runOperation, getOperationExecutions, getOperationExecution,
  deleteOperationExecution, deleteAllOperationExecutions,
  type OperationCard, type ExecutionDetail, type OperationRun,
} from '@/lib/api-client'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { Card, CardContent } from '@/components/ui/Card'
import { Drawer } from '@/components/ui/Drawer'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/Dialog'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { SkeletonTable } from '@/components/ui/Skeleton'
import { toast } from '@/components/ui/Toast'
import { getErrorMessage } from '@/lib/error-utils'
import { t } from '@/lib/i18n'

function fmtTime(ts: number): string {
  return new Date(ts).toLocaleString('zh-CN')
}

// 操作卡片的上次执行摘要三态渲染（§9.1）。
function LastExecutionBadge({ card }: { card: OperationCard }) {
  const le = card.last_execution
  if (!le) {
    return <span className="text-xs text-[var(--color-text-tertiary)]">{t.operations.neverExecuted}</span>
  }
  return (
    <span className="flex min-w-0 items-center gap-1.5 text-xs">
      {le.state === 'running' && (
        <>
          <Loader2 className="h-3.5 w-3.5 animate-spin text-[var(--color-brand-primary)]" />
          <span className="text-[var(--color-text-primary)]">{t.operations.running}</span>
        </>
      )}
      {le.state === 'interrupted' && (
        <>
          <AlertTriangle className="h-3.5 w-3.5 text-[var(--color-text-warning)]" />
          <span className="text-[var(--color-text-warning)]">{t.operations.interrupted}</span>
        </>
      )}
      {le.state === 'finished' && (
        <>
          {le.result === 'failed'
            ? <XCircle className="h-3.5 w-3.5 text-[var(--color-text-error)]" />
            : <CheckCircle className="h-3.5 w-3.5 text-[var(--color-text-success)]" />}
          <Badge variant={le.result === 'failed' ? 'danger' : 'success'}>
            {le.result === 'failed' ? t.operations.finishedFailed : t.operations.finishedSuccess}
          </Badge>
          <span className="text-[var(--color-text-tertiary)]">{fmtTime(le.created_at)}</span>
        </>
      )}
      {le.state !== 'running' && le.state !== 'interrupted' && le.state !== 'finished' && (
        <span className="text-[var(--color-text-tertiary)]">{t.operations.unexpectedState}</span>
      )}
    </span>
  )
}

export default function OperationsPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  // 通知中心"查看关联执行"跳转入口：/operations?execution=<id> 直接打开对应详情抽屉
  const [searchParams] = useSearchParams()
  const urlExecution = searchParams.get('execution')

  // 操作卡片
  const { data: operationsData, isLoading, isError } = useQuery({
    queryKey: ['operations'],
    queryFn: () => getOperations(),
  })
  const cards: OperationCard[] = Array.isArray(operationsData) ? operationsData : []

  // 执行历史
  const { data: executionsData, isLoading: execLoading, isError: execError } = useQuery({
    queryKey: ['operation-executions'],
    queryFn: () => getOperationExecutions(100, 0),
  })
  const executions: ExecutionDetail[] = Array.isArray(executionsData) ? executionsData : []

  // 运行参数：键值对模式（默认，值按字符串传递）或 JSON 模式（高级）
  const [paramsMode, setParamsMode] = useState<Record<string, 'kv' | 'json'>>({})
  const [paramsKV, setParamsKV] = useState<Record<string, Array<{ k: string; v: string }>>>({})
  const [paramsText, setParamsText] = useState<Record<string, string>>({})
  const [paramsOpen, setParamsOpen] = useState<Record<string, boolean>>({})

  // 响应者弹窗（查看绑定列表并跳转服务扩展）
  const [respondersDialogOp, setRespondersDialogOp] = useState<OperationCard | null>(null)

  // 检查是否配置了有效参数
  const hasConfiguredParams = (id: string): boolean => {
    if ((paramsMode[id] ?? 'kv') === 'json') {
      return !!(paramsText[id] ?? '').trim()
    }
    return (paramsKV[id] ?? []).some((r) => r.k.trim() !== '')
  }

  // 由当前参数状态组装运行参数；JSON 非法时返回 null
  const buildParams = (id: string): Record<string, unknown> | null => {
    if ((paramsMode[id] ?? 'kv') === 'json') {
      const raw = (paramsText[id] ?? '').trim()
      if (!raw) return {}
      try {
        const parsed = JSON.parse(raw)
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed as Record<string, unknown>
        return null
      } catch {
        return null
      }
    }
    const obj: Record<string, unknown> = {}
    for (const row of paramsKV[id] ?? []) {
      const k = row.k.trim()
      if (k) obj[k] = row.v
    }
    return obj
  }

  const runMutation = useMutation({
    mutationFn: async ({ id, params }: { id: string; params: Record<string, unknown> }) => {
      // 幂等 key：单次触发时生成一次（同一次 mutation 重试内保持不变）。
      // 不复用旧 key：state 闭包可能捕获运行期间已用过的 key，导致 10 分钟内
      // 再次触发被后端幂等缓存命中而静默返回旧 Execution（忽略新参数）。
      return runOperation(id, params, crypto.randomUUID())
    },
    onSuccess: () => {
      toast.success(t.operations.triggered)
      queryClient.invalidateQueries({ queryKey: ['operations'] })
      queryClient.invalidateQueries({ queryKey: ['operation-executions'] })
      queryClient.invalidateQueries({ queryKey: ['notification-topics'] })
    },
    onError: (err: unknown) => {
      toast.error(getErrorMessage(err, t.operations.triggerFailed))
    },
  })

  const handleRun = (card: OperationCard) => {
    const params = buildParams(card.id)
    if (params === null) {
      toast.error(t.operations.paramInvalid)
      return
    }
    // danger 二次确认（复用现有内联确认模式）
    if (card.button_style === 'danger') {
      setConfirming(card.id)
      return
    }
    doRun(card.id, params)
  }

  const doRun = (id: string, params: Record<string, unknown>) => {
    runMutation.mutate({ id, params })
  }

  const [confirming, setConfirming] = useState<string | null>(null)

  // 执行历史删除（单条/清空，二次确认）
  const [confirmDeleteExec, setConfirmDeleteExec] = useState<ExecutionDetail | null>(null)
  const [confirmClearExecs, setConfirmClearExecs] = useState(false)
  const refreshAfterExecChange = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['operations'] })
    queryClient.invalidateQueries({ queryKey: ['operation-executions'] })
  }, [queryClient])
  const handleDeleteExec = async (ex: ExecutionDetail) => {
    try {
      await deleteOperationExecution(ex.id)
      if (selectedId === ex.id) {
        setSelectedId(null)
        if (urlExecution) navigate('/operations', { replace: true })
      }
      toast.success(t.operations.executionDeleted)
      refreshAfterExecChange()
    } catch (err) {
      toast.error(getErrorMessage(err, t.operations.executionDeleteFailed))
    }
    setConfirmDeleteExec(null)
  }
  const handleClearExecs = async () => {
    try {
      await deleteAllOperationExecutions()
      setSelectedId(null)
      if (urlExecution) navigate('/operations', { replace: true })
      toast.success(t.operations.executionsCleared)
      refreshAfterExecChange()
    } catch (err) {
      toast.error(getErrorMessage(err, t.operations.executionsClearFailed))
    }
    setConfirmClearExecs(false)
  }

  const runningId = runMutation.isPending ? (runMutation.variables?.id ?? null) : null

  // 抽屉（URL 参数联动：/operations?execution=<id> 响应打开/切换对应详情）
  const [selectedId, setSelectedId] = useState<string | null>(urlExecution)
  useEffect(() => {
    if (urlExecution) {
      setSelectedId(urlExecution)
    }
  }, [urlExecution])
  const { data: execDetail } = useQuery({
    queryKey: ['operation-execution', selectedId],
    queryFn: () => getOperationExecution(selectedId!),
    enabled: !!selectedId,
  })

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">{t.operations.title}</h1>
      </div>

      {/* 上区：操作卡片 */}
      <section>
        {isLoading ? (
          <SkeletonTable rows={3} cols={2} />
        ) : isError ? (
          <div className="rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-4 py-3 text-sm text-[var(--color-text-error)]">
            {t.operations.loadFailed}
          </div>
        ) : cards.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] py-10 text-[var(--color-text-secondary)]">
            <Inbox className="mb-2 h-8 w-8 text-[var(--color-text-tertiary)]" />
            <span>{t.operations.empty}</span>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {cards.map((card) => {
              const hasParams = hasConfiguredParams(card.id)
              const primaryRegistrant = (card.registrants ?? [])[0]
              return (
                <Card
                  key={card.id}
                  className="flex flex-col border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] transition-all hover:border-[var(--color-border-focus)] shadow-sm"
                >
                  <CardContent className="flex flex-1 flex-col space-y-2 p-3">
                    {/* 头部：名称 + 危险标识 + 跳转全局扩展编辑页按钮 */}
                    <div className="flex items-start justify-between gap-1.5">
                      <div className="flex min-w-0 items-center gap-1.5">
                        <Settings2 className="h-4 w-4 shrink-0 text-[var(--color-brand-primary)]" />
                        <h3 className="truncate text-sm font-semibold text-[var(--color-text-primary)]" title={card.label}>
                          {card.label}
                        </h3>
                      </div>
                      <div className="flex shrink-0 items-center gap-1">
                        {card.button_style === 'danger' && (
                          <Badge variant="danger" className="px-1 py-0 text-[10px]">
                            高危
                          </Badge>
                        )}
                        {primaryRegistrant && (
                          <button
                            type="button"
                            className="flex h-6 w-6 items-center justify-center rounded text-[var(--color-text-tertiary)] hover:bg-[var(--color-surface-primary)] hover:text-[var(--color-brand-primary)] transition-colors"
                            title={`跳转到全局扩展「${primaryRegistrant}」编辑页`}
                            onClick={() => navigate(`/extensions/${encodeURIComponent(primaryRegistrant)}`)}
                          >
                            <ExternalLink className="h-3.5 w-3.5" />
                          </button>
                        )}
                      </div>
                    </div>

                    {/* 说明（单行截断，节省纵向高度） */}
                    <p className="line-clamp-1 text-[11px] text-[var(--color-text-secondary)]" title={card.description || t.operations.descEmpty}>
                      {card.description || t.operations.descEmpty}
                    </p>

                    {/* 绑定信息：操作 ID + 注册者 + 响应者 */}
                    <div className="space-y-1 rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] p-1.5 text-xs">
                      {/* 绑定操作 ID（显式明确展示，满足用户需求 3） */}
                      <div className="flex items-center justify-between font-mono text-[11px]">
                        <span className="text-[var(--color-text-tertiary)]">绑定操作 ID:</span>
                        <code className="rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 font-semibold text-[var(--color-brand-primary)]">
                          {card.id}
                        </code>
                      </div>

                      {/* 注册者（满足用户需求 2 / 3：带 action 标注，并可直接点击跳转） */}
                      <div className="flex flex-wrap items-center justify-between gap-1 text-[11px]">
                        <span className="text-[var(--color-text-tertiary)]">{t.operations.registrants}:</span>
                        <div className="flex flex-wrap items-center gap-1">
                          {(card.registrants ?? []).length > 0 ? (
                            (card.registrants ?? []).map((r, i) => {
                              const matchingRef = (card.global_refs ?? []).find((g) => g.extension_name === r)
                              return (
                                <button
                                  key={i}
                                  type="button"
                                  onClick={() => navigate(`/extensions/${encodeURIComponent(r)}`)}
                                  title={`点击编辑全局扩展「${r}」`}
                                  className="inline-flex items-center gap-1 rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--color-brand-primary)] hover:underline"
                                >
                                  <span>{r}</span>
                                  {matchingRef?.action_id && (
                                    <span className="text-[10px] text-[var(--color-text-tertiary)]">
                                      ({matchingRef.action_id})
                                    </span>
                                  )}
                                  <ExternalLink className="h-2.5 w-2.5 opacity-60" />
                                </button>
                              )
                            })
                          ) : (
                            <span className="text-[var(--color-text-tertiary)]">-</span>
                          )}
                        </div>
                      </div>

                      {/* 响应者（满足用户需求 4：点击弹出完整绑定列表） */}
                      <div className="flex items-center justify-between text-[11px]">
                        <span className="text-[var(--color-text-tertiary)]">{t.operations.responders}:</span>
                        {card.responder_count > 0 ? (
                          <button
                            type="button"
                            onClick={() => setRespondersDialogOp(card)}
                            className="inline-flex items-center gap-1 rounded bg-[var(--color-bg-tertiary)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--color-text-primary)] hover:text-[var(--color-brand-primary)] hover:underline"
                            title="点击查看所有绑定的服务响应者列表"
                          >
                            <span className="font-semibold text-[var(--color-brand-primary)]">
                              {card.responder_count}
                            </span>
                            <span className="text-[10px] text-[var(--color-text-secondary)]">个响应者</span>
                            <ChevronRight className="h-3 w-3 text-[var(--color-text-tertiary)]" />
                          </button>
                        ) : (
                          <span className="font-mono text-[var(--color-text-tertiary)]">0</span>
                        )}
                      </div>
                    </div>

                    {/* 配置警告 */}
                    {(card.warnings ?? []).length > 0 && (
                      <div className="flex items-start gap-1 rounded border border-[var(--color-border-secondary)] bg-[var(--color-surface-warning)] px-2 py-1 text-[11px] text-[var(--color-text-warning)]">
                        <AlertOctagon className="mt-0.5 h-3 w-3 shrink-0" />
                        <div className="min-w-0">
                          {(card.warnings ?? []).map((w, i) => (
                            <div key={i} className="truncate" title={w}>
                              {w}
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* 上次执行摘要 */}
                    <div className="pt-0.5">
                      <LastExecutionBadge card={card} />
                    </div>

                    {/* 展开的参数面板 */}
                    {paramsOpen[card.id] && (
                      <div className="space-y-2 rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] p-2">
                        <div className="flex items-center justify-between">
                          <span className="text-[10px] text-[var(--color-text-tertiary)]">{t.operations.paramHint}</span>
                          <button
                            type="button"
                            className="text-[10px] text-[var(--color-brand-primary)] hover:underline"
                            onClick={() => setParamsMode((p) => ({ ...p, [card.id]: (p[card.id] ?? 'kv') === 'kv' ? 'json' : 'kv' }))}
                          >
                            {(paramsMode[card.id] ?? 'kv') === 'kv' ? t.operations.paramJSONMode : t.operations.paramKVMode}
                          </button>
                        </div>
                        {(paramsMode[card.id] ?? 'kv') === 'kv' ? (
                          <>
                            {(paramsKV[card.id] ?? []).map((row, ri) => (
                              <div key={ri} className="flex items-center gap-1">
                                <input
                                  value={row.k}
                                  placeholder={t.operations.paramKey}
                                  spellCheck={false}
                                  disabled={runMutation.isPending && runningId === card.id}
                                  onChange={(e) => setParamsKV((p) => ({ ...p, [card.id]: (p[card.id] ?? []).map((r, i) => (i === ri ? { ...r, k: e.target.value } : r)) }))}
                                  className="h-6 w-2/5 rounded border border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] px-1.5 font-mono text-[11px] text-[var(--color-text-primary)] outline-none focus:border-[var(--color-border-focus)]"
                                />
                                <input
                                  value={row.v}
                                  placeholder={t.operations.paramValue}
                                  disabled={runMutation.isPending && runningId === card.id}
                                  onChange={(e) => setParamsKV((p) => ({ ...p, [card.id]: (p[card.id] ?? []).map((r, i) => (i === ri ? { ...r, v: e.target.value } : r)) }))}
                                  className="h-6 min-w-0 flex-1 rounded border border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] px-1.5 text-[11px] text-[var(--color-text-primary)] outline-none focus:border-[var(--color-border-focus)]"
                                />
                                <button
                                  type="button"
                                  title={t.operations.paramRemove}
                                  disabled={runMutation.isPending && runningId === card.id}
                                  onClick={() => setParamsKV((p) => ({ ...p, [card.id]: (p[card.id] ?? []).filter((_, i) => i !== ri) }))}
                                  className="rounded p-0.5 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-error)] disabled:opacity-50"
                                >
                                  <Trash2 className="h-2.5 w-2.5" />
                                </button>
                              </div>
                            ))}
                            <button
                              type="button"
                              disabled={runMutation.isPending && runningId === card.id}
                              onClick={() => setParamsKV((p) => ({ ...p, [card.id]: [...(p[card.id] ?? []), { k: '', v: '' }] }))}
                              className="flex items-center gap-1 text-[11px] text-[var(--color-brand-primary)] hover:underline disabled:opacity-50"
                            >
                              <Plus className="h-3 w-3" /> {t.operations.paramAdd}
                            </button>
                          </>
                        ) : (
                          <textarea
                            value={paramsText[card.id] ?? ''}
                            placeholder={t.operations.paramsDefault}
                            spellCheck={false}
                            disabled={runMutation.isPending && runningId === card.id}
                            onChange={(e) => setParamsText((p) => ({ ...p, [card.id]: e.target.value }))}
                            className="w-full resize-y rounded border border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] px-2 py-1 font-mono text-[11px] text-[var(--color-text-primary)] outline-none focus:border-[var(--color-border-focus)]"
                            rows={2}
                          />
                        )}
                      </div>
                    )}

                    {/* 底部控制条：参数开关 + 运行按钮 */}
                    <div className="mt-auto flex items-center justify-between gap-2 border-t border-[var(--color-border-secondary)] pt-1.5">
                      <button
                        type="button"
                        onClick={() => setParamsOpen((p) => ({ ...p, [card.id]: !p[card.id] }))}
                        className={`inline-flex items-center gap-1 rounded px-2 py-1 text-xs transition-colors ${
                          hasParams
                            ? 'bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)] font-medium'
                            : 'text-[var(--color-text-secondary)] hover:bg-[var(--color-surface-primary)] hover:text-[var(--color-text-primary)]'
                        }`}
                        title="配置运行参数"
                      >
                        <SlidersHorizontal className="h-3 w-3" />
                        <span>{t.operations.paramTitle}</span>
                        {hasParams && (
                          <span className="inline-block h-1.5 w-1.5 rounded-full bg-[var(--color-brand-primary)]" />
                        )}
                        {paramsOpen[card.id] ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                      </button>

                      {confirming === card.id ? (
                        <div className="flex items-center gap-1">
                          <Button
                            variant="danger"
                            size="sm"
                            data-action="run-operation"
                            className="h-7 px-2.5 text-xs font-semibold"
                            onClick={() => {
                              setConfirming(null)
                              const p = buildParams(card.id)
                              if (p !== null) doRun(card.id, p)
                            }}
                            disabled={runMutation.isPending && runningId === card.id}
                          >
                            {runMutation.isPending && runningId === card.id ? (
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            ) : (
                              '确认运行'
                            )}
                          </Button>
                          <Button
                            variant="default"
                            size="sm"
                            className="h-7 px-2 text-xs"
                            onClick={() => setConfirming(null)}
                            disabled={runMutation.isPending && runningId === card.id}
                          >
                            取消
                          </Button>
                        </div>
                      ) : (
                        <Button
                          variant={card.button_style}
                          size="sm"
                          data-action="run-operation"
                          className="h-7 px-3 text-xs"
                          onClick={() => handleRun(card)}
                          disabled={runMutation.isPending && runningId === card.id}
                        >
                          {runMutation.isPending && runningId === card.id ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <PlaySquare className="h-3.5 w-3.5 mr-1" />
                          )}
                          {t.operations.run}
                        </Button>
                      )}
                    </div>
                  </CardContent>
                </Card>
              )
            })}
          </div>
        )}
      </section>

      {/* 下区：执行历史列表 */}
      <section className="rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)]">
        <div className="flex items-center justify-between border-b border-[var(--color-border-secondary)] px-4 py-2.5 text-sm font-medium text-[var(--color-text-primary)]">
          <span>{t.operations.executionsTitle}</span>
          <Button
            variant="default"
            size="sm"
            disabled={executions.length === 0 || execLoading}
            onClick={() => setConfirmClearExecs(true)}
          >
            <Trash2 className="h-3.5 w-3.5" />
            {t.operations.clearExecutions}
          </Button>
        </div>
        {execLoading ? (
          <div className="flex items-center justify-center gap-2 py-8 text-sm text-[var(--color-text-secondary)]">
            <Loader2 className="h-4 w-4 animate-spin" />
            {t.common.loading}
          </div>
        ) : execError ? (
          <div className="px-4 py-8 text-center text-sm text-[var(--color-text-error)]">
            {t.operations.historyLoadFailed}
          </div>
        ) : executions.length === 0 ? (
          <div className="py-8 text-center text-sm text-[var(--color-text-secondary)]">
            {t.operations.executionsEmpty}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t.operations.colOperation}</TableHead>
                <TableHead>{t.operations.colStatus}</TableHead>
                <TableHead>{t.operations.colStart}</TableHead>
                <TableHead>{t.operations.colRuns}</TableHead>
                <TableHead>{t.operations.colSummary}</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {executions.map((ex) => {
                const successCount = ex.runs.filter((r) => r.state === 'success').length
                const hasFailed = ex.runs.some((r) => ['failed', 'timeout', 'canceled', 'killed'].includes(r.state))
                const isFinished = ex.finished_at != null
                const isInterrupted = ex.interrupted_at != null
                const stateLabel = isInterrupted
                  ? t.operations.interrupted
                  : isFinished
                    ? (hasFailed ? t.operations.finishedFailed : t.operations.finishedSuccess)
                    : t.operations.running
                const badgeVariant = isInterrupted
                  ? 'warning'
                  : isFinished
                    ? (hasFailed ? 'danger' : 'success')
                    : 'info'
                return (
                  <TableRow key={ex.id} onClick={() => setSelectedId(ex.id)} className="cursor-pointer">
                    <TableCell className="font-medium">{ex.operation_label}</TableCell>
                    <TableCell>
                      <Badge variant={badgeVariant}>
                        {isInterrupted
                          ? <AlertTriangle className="h-3 w-3" />
                          : isFinished
                            ? (hasFailed ? <XCircle className="h-3 w-3" /> : <CheckCircle className="h-3 w-3" />)
                            : <Loader2 className="h-3 w-3 animate-spin" />}
                        {stateLabel}
                      </Badge>
                    </TableCell>
                    <TableCell className="whitespace-nowrap font-mono text-xs">{fmtTime(ex.created_at)}</TableCell>
                    <TableCell className="font-mono text-xs">{ex.runs.length}</TableCell>
                    <TableCell className="max-w-xs truncate text-xs text-[var(--color-text-secondary)]">
                      {t.operations.summarySuccess.replace('{ok}', String(successCount)).replace('{total}', String(ex.runs.length))}
                    </TableCell>
                    <TableCell>
                      <button
                        title={t.operations.deleteExecution}
                        className="rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-error)]"
                        onClick={(e) => { e.stopPropagation(); setConfirmDeleteExec(ex) }}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </section>

      {/* 删除单条执行记录确认 */}
      {confirmDeleteExec && (
        <div className="fixed left-1/2 top-16 z-50 w-96 -translate-x-1/2 rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-primary)] shadow-xl">
          <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-3 py-2">
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--color-text-error)]" />
              <span className="text-sm font-medium text-[var(--color-text-primary)]">{t.operations.confirmDeleteExecution}</span>
            </div>
          </div>
          <div className="p-3">
            <p className="text-xs text-[var(--color-text-secondary)]">{confirmDeleteExec.operation_label} · {fmtTime(confirmDeleteExec.created_at)}</p>
            <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">{t.operations.confirmDeleteExecDesc}</p>
          </div>
          <div className="flex justify-end gap-2 border-t border-[var(--color-border-primary)] px-3 py-2">
            <Button variant="default" size="sm" onClick={() => setConfirmDeleteExec(null)}>{t.common.cancel}</Button>
            <Button variant="danger" size="sm" onClick={() => handleDeleteExec(confirmDeleteExec)}>
              <Trash2 className="h-3 w-3" />
              {t.operations.deleteExecution}
            </Button>
          </div>
        </div>
      )}

      {/* 清空执行历史确认 */}
      {confirmClearExecs && (
        <div className="fixed left-1/2 top-16 z-50 w-96 -translate-x-1/2 rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-primary)] shadow-xl">
          <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-3 py-2">
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--color-text-error)]" />
              <span className="text-sm font-medium text-[var(--color-text-primary)]">{t.operations.confirmClearExecutions}</span>
            </div>
          </div>
          <div className="p-3">
            <p className="text-xs text-[var(--color-text-tertiary)]">{t.operations.confirmClearExecDesc}</p>
          </div>
          <div className="flex justify-end gap-2 border-t border-[var(--color-border-primary)] px-3 py-2">
            <Button variant="default" size="sm" onClick={() => setConfirmClearExecs(false)}>{t.common.cancel}</Button>
            <Button variant="danger" size="sm" onClick={handleClearExecs}>
              <Trash2 className="h-3 w-3" />
              {t.operations.clearExecutions}
            </Button>
          </div>
        </div>
      )}

      {/* 执行详情抽屉（右侧，不遮挡主列表） */}
      <Drawer
        open={!!selectedId}
        onOpenChange={(v) => {
          if (!v) {
            setSelectedId(null)
            if (urlExecution) navigate('/operations', { replace: true })
          }
        }}
        title={t.operations.drawerTitle}
        description={execDetail ? `#${execDetail.id.slice(0, 8)} · ${execDetail.operation_label}` : undefined}
      >
        {execDetail ? <ExecutionDetailBody exec={execDetail} onOpenTopic={() => navigate(`/notifications?topic=${encodeURIComponent(execDetail.topic_id)}`)} /> : (
          <div className="flex items-center gap-2 py-6 text-sm text-[var(--color-text-tertiary)]">
            <Loader2 className="h-4 w-4 animate-spin" />
            {t.common.loading}
          </div>
        )}
      </Drawer>

      {/* danger 二次确认（内联浮动 Dialog，复用项目现有模式） */}
      {confirming && (() => {
        const card = cards.find((c) => c.id === confirming)
        return (
          <div className="fixed left-1/2 top-16 z-50 w-96 -translate-x-1/2 rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-primary)] shadow-xl">
            <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-3 py-2">
              <div className="flex items-center gap-2">
                <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--color-text-error)]" />
                <span className="text-sm font-medium text-[var(--color-text-primary)]">{t.operations.confirmRun}</span>
              </div>
            </div>
            <div className="p-3">
              <p className="text-xs text-[var(--color-text-secondary)]">
                {card?.label} · {t.operations.confirmRunDesc}
              </p>
            </div>
            <div className="flex justify-end gap-2 border-t border-[var(--color-border-primary)] px-3 py-2">
              <Button variant="default" size="sm" onClick={() => setConfirming(null)}>{t.common.cancel}</Button>
              <Button variant="danger" size="sm" onClick={() => {
                if (!card) return
                setConfirming(null)
                // 确认时重新组装参数（用户可能在确认前修改）：与普通路径共用 buildParams，
                // 避免 KV 模式参数被丢弃、JSON 模式绕过对象校验。
                const params = buildParams(card.id)
                if (params === null) {
                  toast.error(t.operations.paramInvalid)
                  return
                }
                doRun(card.id, params)
              }}>
                {t.operations.run}
              </Button>
            </div>
          </div>
        )
      })()}

      {/* 响应者列表弹窗（REQ-4：弹出绑定的响应者列表，点击能够跳到对应服务扩展页面） */}
      <Dialog
        open={!!respondersDialogOp}
        onOpenChange={(open) => { if (!open) setRespondersDialogOp(null) }}
      >
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Puzzle className="h-4 w-4 text-[var(--color-brand-primary)]" />
              <span>操作响应者绑定列表</span>
            </DialogTitle>
            <DialogDescription>
              操作「{respondersDialogOp?.label}」（绑定操作 ID: <code className="font-mono font-semibold text-[var(--color-brand-primary)]">{respondersDialogOp?.id}</code>）共绑定了 {respondersDialogOp?.responders?.length ?? respondersDialogOp?.responder_count ?? 0} 个服务扩展响应者。
            </DialogDescription>
          </DialogHeader>

          <div className="mt-2 max-h-[60vh] overflow-y-auto space-y-2">
            {(!respondersDialogOp?.responders || respondersDialogOp.responders.length === 0) ? (
              <div className="py-8 text-center text-xs text-[var(--color-text-secondary)]">
                暂无服务扩展响应此操作
              </div>
            ) : (
              <div className="divide-y divide-[var(--color-border-secondary)] rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)]">
                {respondersDialogOp.responders.map((resp, idx) => (
                  <div key={idx} className="flex items-center justify-between p-3 text-xs">
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <span className="text-[var(--color-text-tertiary)]">服务:</span>
                        <span className="font-semibold text-[var(--color-text-primary)]">{resp.service_name}</span>
                      </div>
                      <div className="flex items-center gap-2 font-mono text-[11px]">
                        <span className="text-[var(--color-text-tertiary)]">扩展:</span>
                        <span className="text-[var(--color-brand-primary)]">{resp.extension_name}</span>
                        <span className="text-[var(--color-text-tertiary)]">·</span>
                        <span className="text-[var(--color-text-tertiary)]">Action:</span>
                        <span className="text-[var(--color-text-secondary)]">{resp.action_id}</span>
                      </div>
                    </div>
                    <Button
                      variant="default"
                      size="sm"
                      className="h-7 text-xs flex items-center gap-1 shrink-0"
                      onClick={() => {
                        const svc = resp.service_name
                        setRespondersDialogOp(null)
                        navigate(`/services/${encodeURIComponent(svc)}?tab=extensions`)
                      }}
                      title={`前往服务「${resp.service_name}」扩展页面`}
                    >
                      <span>前往服务扩展</span>
                      <ExternalLink className="h-3 w-3" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <DialogFooter>
            <Button
              variant="default"
              size="sm"
              onClick={() => setRespondersDialogOp(null)}
            >
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// 执行详情抽屉正文：Execution 信息 + Global/Service 两阶段分组。
function ExecutionDetailBody({ exec, onOpenTopic }: { exec: ExecutionDetail; onOpenTopic: () => void }) {
  const globalRuns = exec.runs.filter((r) => r.phase === 'global')
  const serviceRuns = exec.runs.filter((r) => r.phase === 'service')

  return (
    <div className="space-y-5">
      {/* Execution 信息 */}
      <div className="space-y-1 rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] p-3 text-xs">
        <div className="flex items-center justify-between">
          <span className="text-[var(--color-text-tertiary)]">ID</span>
          <span className="font-mono text-[var(--color-text-primary)]">{exec.id}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-[var(--color-text-tertiary)]">Operation</span>
          <span className="font-mono text-[var(--color-text-primary)]">{exec.operation_label}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-[var(--color-text-tertiary)]">Run</span>
          <span className="font-mono text-[var(--color-text-primary)]">{exec.runs.length}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-[var(--color-text-tertiary)]">开始</span>
          <span className="font-mono text-[var(--color-text-primary)]">{fmtTime(exec.created_at)}</span>
        </div>
        {exec.finished_at && (
          <div className="flex items-center justify-between">
            <span className="text-[var(--color-text-tertiary)]">完成</span>
            <span className="font-mono text-[var(--color-text-primary)]">{fmtTime(exec.finished_at)}</span>
          </div>
        )}
        {exec.interrupted_at && (
          <div className="flex items-center justify-between text-[var(--color-text-warning)]">
            <span>中断</span>
            <span className="font-mono">{fmtTime(exec.interrupted_at)}</span>
          </div>
        )}
        <div className="flex items-center justify-between">
          <span className="text-[var(--color-text-tertiary)]">Topic</span>
          <button onClick={onOpenTopic} className="flex items-center gap-1 text-[var(--color-brand-primary)] hover:underline">
            {exec.topic_id.slice(0, 8)}
            <ExternalLink className="h-3 w-3" />
          </button>
        </div>
      </div>

      <RunGroup title={t.operations.phaseGlobal} runs={globalRuns} />
      <RunGroup title={t.operations.phaseService} runs={serviceRuns} />
    </div>
  )
}

// 每阶段 Run 列表（服务/扩展/action/状态七种/日志链接）。
function RunGroup({ title, runs }: { title: string; runs: OperationRun[] }) {
  const navigate = useNavigate()
  if (runs.length === 0) return null
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between px-1 text-xs font-medium text-[var(--color-text-secondary)]">
        <span>{title}</span>
        <span className="font-mono text-[var(--color-text-tertiary)]">{runs.length}</span>
      </div>
      <div className="space-y-1.5">
        {runs.map((r) => (
          <div key={r.run_id} className="rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] p-2.5">
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="truncate font-mono text-xs text-[var(--color-text-primary)]">{r.extension_name}</span>
                  <Badge variant="secondary" className="font-mono">{r.action_id}</Badge>
                </div>
                {r.service_name && <div className="mt-0.5 truncate font-mono text-[10px] text-[var(--color-text-tertiary)]">{r.service_name}</div>}
              </div>
              <div className="flex shrink-0 items-center gap-1.5">
                <RunStateBadge state={r.state} />
                <Button variant="default" size="sm" title={t.operations.runLog} onClick={() => navigate(`/extensions/${encodeURIComponent(r.extension_name)}`)}>
                  <FileText className="h-3 w-3" />
                  {t.operations.runLog}
                </Button>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

// 七种任务状态徽标（与既有页面语义一致）。
function RunStateBadge({ state }: { state: string }) {
  const map: Record<string, 'default' | 'success' | 'warning' | 'danger' | 'info' | 'secondary'> = {
    success: 'success',
    failed: 'danger',
    killed: 'danger',
    timeout: 'warning',
    canceled: 'warning',
    running: 'info',
    pending: 'default',
  }
  return (
    <Badge variant={map[state] ?? 'default'}>
      {state === 'running' || state === 'pending'
        ? <Loader2 className="h-3 w-3 animate-spin" />
        : state === 'success' ? <CheckCircle className="h-3 w-3" />
          : state === 'failed' ? <XCircle className="h-3 w-3" />
            : <Fragment>{null}</Fragment>}
      {state}
    </Badge>
  )
}