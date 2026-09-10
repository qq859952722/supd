// 节点 09-5：通知中心页（§八 / v5 单一混排列表 + 页内展开详情）。
// 混排单一 Topic 列表，按后端返回（最近活动倒序）；顶部筛选；打开详情即 markTopicRead；
// 长轮询刷新由全局 changes 通道（铃铛）驱动，本页不另建轮询通道。

import { useState, useMemo, useEffect, useCallback, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams, useNavigate } from 'react-router'
import {
  Settings2, Server, Puzzle, AlertOctagon, ChevronDown, ChevronRight, Bell,
  Loader2, Trash2, CheckCheck, Eraser, AlertTriangle,
} from 'lucide-react'
import {
  getNotificationTopics, getNotificationTopic, markTopicRead, markAllRead,
  deleteTopic, clearAllTopics,
  apiGet,
  type TopicItem, type Notification, type TopicListResponse,
} from '@/lib/api-client'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { Select } from '@/components/ui/Select'
import { toast } from '@/components/ui/Toast'
import { getErrorMessage } from '@/lib/error-utils'
import { t } from '@/lib/i18n'

function fmtTime(ts: number): string {
  return new Date(ts).toLocaleString('zh-CN')
}

function levelColor(level: string): string {
  switch (level) {
    case 'success': return 'var(--color-text-success)'
    case 'warning': return 'var(--color-text-warning)'
    case 'error': return 'var(--color-text-error)'
    default: return 'var(--color-text-info)'
  }
}

function KindIcon({ kind }: { kind: string }) {
  const cls = 'h-4 w-4 shrink-0 text-[var(--color-text-secondary)]'
  switch (kind) {
    case 'operation': return <Settings2 className={cls} />
    case 'service': return <Server className={cls} />
    case 'extension': return <Puzzle className={cls} />
    default: return <AlertOctagon className={cls} />
  }
}

function kindLabel(kind: string): string {
  switch (kind) {
    case 'operation': return t.notifications.kindOperation
    case 'service': return t.notifications.kindService
    case 'extension': return t.notifications.kindExtension
    case 'system': return t.notifications.kindSystem
    default: return kind
  }
}

type NotificationFilter = {
  unreadOnly: boolean
  level: string
  kind: string
  source_type: string
  service_name: string
}

const EMPTY_FILTER: NotificationFilter = { unreadOnly: false, level: '', kind: '', source_type: '', service_name: '' }

export default function NotificationsPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const urlTopic = searchParams.get('topic')

  const [filter, setFilter] = useState<NotificationFilter>(EMPTY_FILTER)
  const [selectedId, setSelectedId] = useState<string | null>(urlTopic)
  useEffect(() => {
    if (urlTopic) {
      setSelectedId(urlTopic)
    }
  }, [urlTopic])

  // 服务下拉复用 GET /api/services（与全局导航同一 queryKey）
  const { data: servicesData } = useQuery({
    queryKey: ['services-list'],
    queryFn: () => apiGet<{ services: Array<{ name: string; status: string }> }>('/api/services'),
    refetchInterval: 60_000,
  })
  const services = useMemo(() => servicesData?.services ?? [], [servicesData])

  // 混排 Topic 列表（server 已按最近活动倒序）
  const listQueryKey = ['notification-topics', 'list', filter]
  const { data: listData, refetch, isFetching, isPending: listLoading, isError: listError } = useQuery<TopicListResponse>({
    queryKey: listQueryKey,
    queryFn: () => getNotificationTopics({
      unread: filter.unreadOnly || undefined,
      level: filter.level || undefined,
      kind: filter.kind || undefined,
      source_type: filter.source_type || undefined,
      service_name: filter.service_name || undefined,
    }),
  })
  const topics: TopicItem[] = listData?.topics ?? []
  const storeError = listData?.store_error ?? null

  // 展开详情（seq 正序不可变通知，打开即 markTopicRead）
  const [sinceSeq, setSinceSeq] = useState(0)
  const [accumulated, setAccumulated] = useState<Notification[]>([])
  const { data: detailData, isFetching: detailFetching } = useQuery({
    queryKey: ['notification-topic', selectedId, sinceSeq],
    queryFn: () => getNotificationTopic(selectedId!, sinceSeq || undefined, 200),
    enabled: !!selectedId,
  })

  // 标记已读时机：收起详情或切换到另一条时（handleExpand 内），
  // 未读筛选下打开消息时列表项保留，避免"点击后消息立即消失而看不到内容"。
  const invalidateAll = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['notification-topics'] })
    queryClient.invalidateQueries({ queryKey: ['notification-topics-bell'] })
  }, [queryClient])

  const handleExpand = useCallback((id: string) => {
    // 副作用不放进 setState updater（StrictMode 下 updater 会双调用，产生重复请求）。
    if (selectedId === id) {
      // 收起详情：内容已读，此时才标已读并刷新列表与红点
      markTopicRead(id).then(invalidateAll).catch(() => { /* 静默 */ })
      setSelectedId(null)
      if (urlTopic) {
        navigate('/notifications', { replace: true })
      }
      return
    }
    if (selectedId) {
      // 直接切换到另一条：前一条视为已读
      markTopicRead(selectedId).then(invalidateAll).catch(() => { /* 静默 */ })
    }
    setSelectedId(id)
  }, [selectedId, invalidateAll])

  // 卸载兜底：展开详情后直接离开页面/路由切换时补标已读，避免红点滞留。
  const selectedIdRef = useRef(selectedId)
  useEffect(() => { selectedIdRef.current = selectedId }, [selectedId])
  useEffect(() => {
    return () => {
      const cur = selectedIdRef.current
      if (cur) {
        markTopicRead(cur).then(invalidateAll).catch(() => { /* 静默 */ })
      }
    }
    // 仅卸载时执行；invalidateAll/queryClient 引用稳定
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 分页累加 + 切换主题/筛选变更时重置
  useEffect(() => {
    setSinceSeq(0)
    setAccumulated([])
  }, [selectedId])
  useEffect(() => {
    if (!detailData) return
    setAccumulated((prev) => {
      const seen = new Set(prev.map((n) => n.id))
      const add = (detailData.notifications ?? []).filter((n) => !seen.has(n.id))
      return add.length ? [...prev, ...add] : prev
    })
  }, [detailData])

  const hasMore = detailData?.has_more ?? false
  const effectiveNextSeq = detailData?.next_seq ?? 0

  // 全部已读
  const [markingAll, setMarkingAll] = useState(false)
  const handleMarkAllRead = async () => {
    setMarkingAll(true)
    try {
      await markAllRead()
      invalidateAll()
    } catch (err) {
      toast.error(getErrorMessage(err, t.notifications.title))
    } finally {
      setMarkingAll(false)
    }
  }

  // 删除单个（二次确认）
  const [confirmDelete, setConfirmDelete] = useState<TopicItem | null>(null)
  const handleDelete = async (item: TopicItem) => {
    try {
      await deleteTopic(item.id)
      if (selectedId === item.id) {
        setSelectedId(null)
        if (urlTopic) navigate('/notifications', { replace: true })
      }
      invalidateAll()
    } catch (err) {
      toast.error(getErrorMessage(err, t.notifications.delete))
    }
    setConfirmDelete(null)
  }

  // 清空全部（二次确认）
  const [confirmClear, setConfirmClear] = useState(false)
  const handleClearAll = async () => {
    try {
      await clearAllTopics()
      setSelectedId(null)
      if (urlTopic) navigate('/notifications', { replace: true })
      invalidateAll()
    } catch (err) {
      toast.error(getErrorMessage(err, t.notifications.clearAll))
    }
    setConfirmClear(false)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">{t.notifications.title}</h1>
        <div className="flex items-center gap-2">
          <Button variant="default" size="sm" onClick={handleMarkAllRead} disabled={markingAll || topics.length === 0}>
            {markingAll ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CheckCheck className="h-3.5 w-3.5" />}
            {t.notifications.markAllRead}
          </Button>
          <Button variant="default" size="sm" onClick={() => setConfirmClear(true)} disabled={topics.length === 0}>
            <Eraser className="h-3.5 w-3.5" />
            {t.notifications.clearAll}
          </Button>
        </div>
      </div>

      {/* 顶部筛选 */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex items-center rounded-lg border border-[var(--color-border-secondary)] bg-[var(--color-surface-secondary)] p-0.5">
          <button
            onClick={() => setFilter((f) => ({ ...f, unreadOnly: false }))}
            className={`rounded-md px-3 py-1 text-sm ${!filter.unreadOnly ? 'bg-[var(--color-surface-primary)] text-[var(--color-text-primary)]' : 'text-[var(--color-text-secondary)]'}`}
          >
            {t.notifications.filterAll}
          </button>
          <button
            onClick={() => setFilter((f) => ({ ...f, unreadOnly: true }))}
            className={`rounded-md px-3 py-1 text-sm ${filter.unreadOnly ? 'bg-[var(--color-surface-primary)] text-[var(--color-text-primary)]' : 'text-[var(--color-text-secondary)]'}`}
          >
            {t.notifications.filterUnread}
          </button>
        </div>
        <Select
          className="w-28"
          placeholder={t.notifications.levelAll}
          value={filter.level}
          onChange={(e) => setFilter((f) => ({ ...f, level: e.target.value }))}
          options={[
            { value: 'info', label: t.notifications.levelInfo },
            { value: 'success', label: t.notifications.levelSuccess },
            { value: 'warning', label: t.notifications.levelWarning },
            { value: 'error', label: t.notifications.levelError },
          ]}
        />
        <Select
          className="w-28"
          placeholder={t.notifications.kindAll}
          value={filter.kind}
          onChange={(e) => setFilter((f) => ({ ...f, kind: e.target.value }))}
          options={[
            { value: 'operation', label: t.notifications.kindOperation },
            { value: 'service', label: t.notifications.kindService },
            { value: 'extension', label: t.notifications.kindExtension },
            { value: 'system', label: t.notifications.kindSystem },
          ]}
        />
        <Select
          className="w-28"
          placeholder={t.notifications.sourceAll}
          value={filter.source_type}
          onChange={(e) => setFilter((f) => ({ ...f, source_type: e.target.value }))}
          options={[
            { value: 'service', label: t.notifications.kindService },
            { value: 'extension', label: t.notifications.kindExtension },
            { value: 'system', label: t.notifications.kindSystem },
          ]}
        />
        <Select
          className="w-40"
          placeholder={t.notifications.serviceAll}
          value={filter.service_name}
          onChange={(e) => setFilter((f) => ({ ...f, service_name: e.target.value }))}
          options={services.map((s) => ({ value: s.name, label: s.name }))}
        />
        <Button variant="default" size="sm" onClick={() => refetch()} disabled={isFetching}>
          <Loader2 className={`h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          {t.common.refresh}
        </Button>
      </div>

      {/* store_error 错误条幅（方案 A：字段消失自动隐藏；不展示底层错误细节） */}
      {storeError && (
        <div className="flex items-center gap-2 rounded-md border border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-4 py-2 text-sm text-[var(--color-text-error)]">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span>{t.notifications.storeError}</span>
        </div>
      )}

      {/* 混排列表 + 页内详情 */}
      <div className="rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)]">
        {listLoading ? (
          <div className="flex items-center justify-center gap-2 py-10 text-sm text-[var(--color-text-secondary)]">
            <Loader2 className="h-4 w-4 animate-spin" />
            {t.common.loading}
          </div>
        ) : listError ? (
          <div className="flex flex-col items-center py-10 text-sm text-[var(--color-text-error)]">
            <AlertTriangle className="mb-2 h-8 w-8" />
            <span>{t.notifications.loadFailed}</span>
          </div>
        ) : topics.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-10 text-[var(--color-text-secondary)]">
            <Bell className="mb-2 h-8 w-8 text-[var(--color-text-tertiary)]" />
            <span>{t.notifications.empty}</span>
          </div>
        ) : (
          <div className="divide-y divide-[var(--color-border-secondary)]">
            {topics.map((item) => {
              const isSelected = item.id === selectedId
              const unread = item.last_seq > item.read_seq
              const time = item.last_activity_at ?? item.created_at
              return (
                <div key={item.id}>
                  <button
                    onClick={() => handleExpand(item.id)}
                    className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-[var(--color-surface-hover)]"
                  >
                    <KindIcon kind={item.kind} />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-sm font-medium text-[var(--color-text-primary)]">{item.source_name}</span>
                        <Badge variant="secondary">{kindLabel(item.kind)}</Badge>
                        {unread && <span className="h-2 w-2 shrink-0 rounded-full bg-[var(--color-accent-danger)]" title={t.notifications.unreadDot} />}
                      </div>
                      <div className="mt-0.5 truncate text-xs text-[var(--color-text-secondary)]">
                        {item.last_content || t.notifications.noDetail}
                      </div>
                    </div>
                    <div className="flex shrink-0 flex-col items-end gap-1">
                      <span className="font-mono text-xs text-[var(--color-text-tertiary)]">{fmtTime(time)}</span>
                      <span className="text-[10px] text-[var(--color-text-tertiary)]">{t.notifications.notificationsCount.replace('{n}', String(item.last_seq))}</span>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      {item.execution_id && (
                        <button
                          title={t.operations.topicLink}
                          className="rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-brand-primary)]"
                          onClick={(e) => {
                            // 跳转操作中心并打开该 Topic 对应的执行详情（与执行抽屉的"关联通知"链接对称）
                            e.stopPropagation()
                            navigate(`/operations?execution=${encodeURIComponent(item.execution_id!)}`)
                          }}
                        >
                          <AlertOctagon className="h-3 w-3" />
                        </button>
                      )}
                      <button
                        title={t.notifications.delete}
                        className="rounded p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-error)]"
                        onClick={(e) => { e.stopPropagation(); setConfirmDelete(item) }}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                      {isSelected ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                    </div>
                  </button>

                  {/* 页内展开详情（seq 正序不可变通知列表） */}
                  {isSelected && (
                    <div className="border-t border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] px-4 py-3">
                      <div className="mb-2 flex items-center justify-between">
                        <span className="text-xs font-medium text-[var(--color-text-secondary)]">
                          {t.notifications.detail} · {item.source_name} · {t.notifications.notificationsCount.replace('{n}', String(item.last_seq))}
                        </span>
                        <Button variant="default" size="sm" onClick={() => setConfirmDelete(item)}>
                          <Trash2 className="h-3 w-3" />
                          {t.notifications.delete}
                        </Button>
                      </div>
                      {detailFetching && accumulated.length === 0 ? (
                        <div className="flex items-center gap-2 py-3 text-sm text-[var(--color-text-tertiary)]">
                          <Loader2 className="h-4 w-4 animate-spin" />
                          {t.common.loading}
                        </div>
                      ) : accumulated.length === 0 ? (
                        <div className="py-3 text-sm text-[var(--color-text-tertiary)]">{t.notifications.noDetail}</div>
                      ) : (
                        <div className="space-y-1.5">
                          {accumulated.map((n) => (
                            <div key={n.id} className="flex items-start gap-2.5 rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] px-3 py-2">
                              <span className="mt-1 h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: levelColor(n.level) }} />
                              <div className="min-w-0 flex-1">
                                <div className="text-sm text-[var(--color-text-primary)] break-words whitespace-pre-wrap">
                                  {n.content}
                                </div>
                                <div className="mt-0.5 flex items-center gap-2 text-[10px] text-[var(--color-text-tertiary)]">
                                  <span className="font-mono">#{n.seq}</span>
                                  <span>{fmtTime(n.created_at)}</span>
                                  {n.source_type && <span>{n.source_type}</span>}
                                </div>
                              </div>
                            </div>
                          ))}
                          {hasMore && (
                            <div className="flex justify-center">
                              <Button variant="default" size="sm" onClick={() => setSinceSeq(effectiveNextSeq)}>
                                {t.notifications.loadMore}
                              </Button>
                            </div>
                          )}
                          {!hasMore && topics.length > 0 && (
                            <div className="py-1 text-center text-xs text-[var(--color-text-tertiary)]">{t.notifications.noMore}</div>
                          )}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* 删除确认（内联浮动 Dialog） */}
      {confirmDelete && (
        <div className="fixed left-1/2 top-16 z-50 w-96 -translate-x-1/2 rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-primary)] shadow-xl">
          <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-3 py-2">
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--color-text-error)]" />
              <span className="text-sm font-medium text-[var(--color-text-primary)]">{t.notifications.confirmDelete}</span>
            </div>
          </div>
          <div className="p-3">
            <p className="text-xs text-[var(--color-text-secondary)]">{confirmDelete.source_name}</p>
            <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">{t.notifications.confirmDeleteDesc}</p>
          </div>
          <div className="flex justify-end gap-2 border-t border-[var(--color-border-primary)] px-3 py-2">
            <Button variant="default" size="sm" onClick={() => setConfirmDelete(null)}>{t.common.cancel}</Button>
            <Button variant="danger" size="sm" onClick={() => handleDelete(confirmDelete)} disabled={isFetching}>
              <Trash2 className="h-3 w-3" />
              {t.notifications.delete}
            </Button>
          </div>
        </div>
      )}

      {/* 清空确认 */}
      {confirmClear && (
        <div className="fixed left-1/2 top-16 z-50 w-96 -translate-x-1/2 rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-primary)] shadow-xl">
          <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-3 py-2">
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--color-text-error)]" />
              <span className="text-sm font-medium text-[var(--color-text-primary)]">{t.notifications.confirmClearAll}</span>
            </div>
          </div>
          <div className="p-3">
            <p className="text-xs text-[var(--color-text-tertiary)]">{t.notifications.confirmDeleteDesc}</p>
          </div>
          <div className="flex justify-end gap-2 border-t border-[var(--color-border-primary)] px-3 py-2">
            <Button variant="default" size="sm" onClick={() => setConfirmClear(false)}>{t.common.cancel}</Button>
            <Button variant="danger" size="sm" onClick={handleClearAll}>
              <Eraser className="h-3 w-3" />
              {t.notifications.clearAll}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}