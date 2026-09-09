// 节点 09-4：操作中心页（§九 / §十二.5.6 / v5 同页两区）。
// 上区：操作卡片区（触发 + 上次执行摘要）；下区：执行历史列表 + 右侧详情抽屉。
// 卡片字段与 GET /api/operations 真实 JSON 对齐；时间戳为 epoch 毫秒 number。

import { useState, useCallback, Fragment } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import {
  PlaySquare, Settings2, Loader2, CheckCircle, XCircle, AlertTriangle,
  ChevronRight, ChevronDown, FileText, ExternalLink, Inbox, AlertOctagon,
} from 'lucide-react'
import {
  getOperations, runOperation, getOperationExecutions, getOperationExecution,
  type OperationCard, type ExecutionDetail, type OperationRun,
} from '@/lib/api-client'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { Card, CardContent } from '@/components/ui/Card'
import { Drawer } from '@/components/ui/Drawer'
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

  // 操作卡片
  const { data: operationsData, isLoading, isError } = useQuery({
    queryKey: ['operations'],
    queryFn: () => getOperations(),
  })
  const cards: OperationCard[] = Array.isArray(operationsData) ? operationsData : []

  // 执行历史
  const { data: executionsData } = useQuery({
    queryKey: ['operation-executions'],
    queryFn: () => getOperationExecutions(100, 0),
  })
  const executions: ExecutionDetail[] = Array.isArray(executionsData) ? executionsData : []

  // 每张卡片的 JSON 参数注入 + 折叠
  const [paramsText, setParamsText] = useState<Record<string, string>>({})
  const [paramsOpen, setParamsOpen] = useState<Record<string, boolean>>({})

  // 幂等 key：触发时生成，失败重试复用同一 key（后端 TTL 10 分钟）
  const [idemKeys, setIdemKeys] = useState<Record<string, string>>({})

  const runMutation = useMutation({
    mutationFn: async ({ id, params, idemKey }: { id: string; params: Record<string, unknown>; idemKey: string }) =>
      runOperation(id, params, idemKey),
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

  const handleRun = useCallback((card: OperationCard) => {
    const raw = (paramsText[card.id] ?? '').trim()
    let params: Record<string, unknown> = {}
    if (raw) {
      try {
        const parsed = JSON.parse(raw)
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          params = parsed as Record<string, unknown>
        } else {
          toast.error(t.operations.paramInvalid)
          return
        }
      } catch {
        toast.error(t.operations.paramInvalid)
        return
      }
    }
    // danger 二次确认（复用现有内联确认模式）
    if (card.button_style === 'danger') {
      setConfirming(card.id)
      return
    }
    doRun(card.id, params)
  }, [paramsText])

  const doRun = (id: string, params: Record<string, unknown>) => {
    const idemKey = idemKeys[id] ?? crypto.randomUUID()
    setIdemKeys((prev) => ({ ...prev, [id]: idemKey }))
    runMutation.mutate({ id, params, idemKey })
  }

  const [confirming, setConfirming] = useState<string | null>(null)

  const runningId = runMutation.isPending ? (runMutation.variables?.id ?? null) : null

  // 抽屉
  const [selectedId, setSelectedId] = useState<string | null>(null)
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
            {t.operations.triggerFailed}
          </div>
        ) : cards.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] py-10 text-[var(--color-text-secondary)]">
            <Inbox className="mb-2 h-8 w-8 text-[var(--color-text-tertiary)]" />
            <span>{t.operations.empty}</span>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
            {cards.map((card) => (
              <Card key={card.id} className="flex flex-col">
                <CardContent className="flex flex-1 flex-col gap-3 p-4">
                  {/* 头部：名称 + 上次执行 */}
                  <div className="flex items-center justify-between gap-2">
                    <h3 className="flex min-w-0 items-center gap-1.5 text-sm font-semibold text-[var(--color-text-primary)]">
                      <Settings2 className="h-4 w-4 shrink-0 text-[var(--color-brand-primary)]" />
                      <span className="truncate">{card.label}</span>
                    </h3>
                  </div>

                  {/* 说明 */}
                  <p className="text-xs text-[var(--color-text-secondary)]">
                    {card.description || t.operations.descEmpty}
                  </p>

                  {/* 注册者 / 响应者 */}
                  <div className="flex flex-col gap-1 text-xs text-[var(--color-text-tertiary)]">
                    <div className="flex flex-wrap items-center gap-1">
                      <span>{t.operations.registrants}:</span>
                      {card.registrants.length > 0 ? (
                        card.registrants.map((r, i) => (
                          <Badge key={i} variant="secondary" className="font-mono">{r}</Badge>
                        ))
                      ) : (
                        <span className="text-[var(--color-text-tertiary)]">-</span>
                      )}
                    </div>
                    <div className="flex items-center gap-1">
                      <span>{t.operations.responders}:</span>
                      <span className="font-mono text-[var(--color-text-secondary)]">{card.responder_count}</span>
                    </div>
                  </div>

                  {/* 配置警告 */}
                  {card.warnings.length > 0 && (
                    <div className="flex items-start gap-1.5 rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-warning)] px-2.5 py-1.5 text-xs text-[var(--color-text-warning)]">
                      <AlertOctagon className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                      <div>
                        <div className="font-medium">{t.operations.warnings}</div>
                        {card.warnings.map((w, i) => <div key={i} className="break-all">{w}</div>)}
                      </div>
                    </div>
                  )}

                  {/* 上次执行摘要 */}
                  <div className="mt-auto pt-1">
                    <LastExecutionBadge card={card} />
                  </div>

                  {/* 参数折叠输入 + 运行按钮 */}
                  <div className="rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)]">
                    <button
                      type="button"
                      onClick={() => setParamsOpen((p) => ({ ...p, [card.id]: !p[card.id] }))}
                      className="flex w-full items-center justify-between px-2.5 py-1.5 text-left text-xs text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)]"
                    >
                      <span>{t.operations.paramsPlaceholder}</span>
                      {paramsOpen[card.id] ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                    </button>
                    {paramsOpen[card.id] && (
                      <textarea
                        value={paramsText[card.id] ?? t.operations.paramsDefault}
                        spellCheck={false}
                        disabled={runMutation.isPending && runningId === card.id}
                        onChange={(e) => setParamsText((p) => ({ ...p, [card.id]: e.target.value }))}
                        className="w-full resize-y rounded-b-md border-t border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] px-2.5 py-1.5 font-mono text-xs text-[var(--color-text-primary)] outline-none focus:border-[var(--color-border-focus)]"
                        rows={3}
                      />
                    )}
                  </div>

                  <Button
                    variant={card.button_style}
                    size="md"
                    onClick={() => handleRun(card)}
                    disabled={runMutation.isPending && runningId === card.id}
                  >
                    {runMutation.isPending && runningId === card.id
                      ? <Loader2 className="h-4 w-4 animate-spin" />
                      : <PlaySquare className="h-4 w-4" />}
                    {t.operations.run}
                  </Button>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </section>

      {/* 下区：执行历史列表 */}
      <section className="rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)]">
        <div className="border-b border-[var(--color-border-secondary)] px-4 py-2.5 text-sm font-medium text-[var(--color-text-primary)]">
          {t.operations.executionsTitle}
        </div>
        {executions.length === 0 ? (
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
              </TableRow>
            </TableHeader>
            <TableBody>
              {executions.map((ex) => {
                const successCount = ex.runs.filter((r) => r.state === 'success').length
                const stateLabel = ex.interrupted_at
                  ? t.operations.interrupted
                  : ex.finished_at ? t.operations.finishedSuccess : t.operations.running
                return (
                  <TableRow key={ex.id} onClick={() => setSelectedId(ex.id)} className="cursor-pointer">
                    <TableCell className="font-medium">{ex.operation_label}</TableCell>
                    <TableCell>
                      <Badge variant={ex.interrupted_at ? 'warning' : ex.finished_at ? 'success' : 'info'}>
                        {ex.interrupted_at
                          ? <AlertTriangle className="h-3 w-3" />
                          : ex.finished_at
                            ? <CheckCircle className="h-3 w-3" />
                            : <Loader2 className="h-3 w-3 animate-spin" />}
                        {stateLabel}
                      </Badge>
                    </TableCell>
                    <TableCell className="whitespace-nowrap font-mono text-xs">{fmtTime(ex.created_at)}</TableCell>
                    <TableCell className="font-mono text-xs">{ex.runs.length}</TableCell>
                    <TableCell className="max-w-xs truncate text-xs text-[var(--color-text-secondary)]">
                      {t.operations.summarySuccess.replace('{ok}', String(successCount)).replace('{total}', String(ex.runs.length))}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </section>

      {/* 执行详情抽屉（右侧，不遮挡主列表） */}
      <Drawer
        open={!!selectedId}
        onOpenChange={(v) => { if (!v) setSelectedId(null) }}
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
              <Button variant="danger" size="sm" onClick={() => { const c = card; setConfirming(null); if (c) { const raw = (paramsText[c.id] ?? '').trim(); let params: Record<string, unknown> = {}; if (raw) { try { params = JSON.parse(raw) as Record<string, unknown> } catch { toast.error(t.operations.paramInvalid); return } } doRun(c.id, params) } }}>
                {t.operations.run}
              </Button>
            </div>
          </div>
        )
      })()}
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