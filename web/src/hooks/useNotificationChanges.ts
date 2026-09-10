// 节点 09-3：全局通知 changes 长轮询一致性通道。
// 铃铛挂载时启动；新 seq / epoch 不符时 invalidate 共享 queryKey ['notification-topics']，
// 通知中心列表随之刷新——通知中心不另建轮询通道。
// 页面不可见暂停、恢复继续；429/网络错误静默指数退避（1s/2s/4s…上限30s），
// 红点保持最后已知值，不弹 toast；请求取消（卸载/暂停）不重试。

import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getNotificationTopics, pollNotificationChanges } from '@/lib/api-client'

export function useNotificationChanges(): { hasUnread: boolean } {
  const [hasUnread, setHasUnread] = useState(false)
  const [epoch, setEpoch] = useState('')
  const [seq, setSeq] = useState(0)
  const [topicVersion, setTopicVersion] = useState(0)
  const epochRef = useRef('')
  const seqRef = useRef(0)
  epochRef.current = epoch
  seqRef.current = seq
  const queryClient = useQueryClient()

  // 铃铛红点依据：任何 Topic last_seq > read_seq 即未读。
  const { data: topicsData } = useQuery({
    queryKey: ['notification-topics-bell', topicVersion],
    queryFn: () => getNotificationTopics({}),
    staleTime: 30_000,
  })

  useEffect(() => {
    if (!topicsData) return
    // topics 可能为 null（Go nil slice JSON 序列化），判空防止整页崩溃
    setHasUnread((topicsData.topics ?? []).some((tm) => tm.last_seq > tm.read_seq))
  }, [topicsData])

  useEffect(() => {
    let cancelled = false
    let abort: AbortController | null = null
    let timer: ReturnType<typeof setTimeout> | null = null
    let delayMs = 1000

    const clearTimer = () => {
      if (timer) {
        clearTimeout(timer)
        timer = null
      }
    }

    const run = async () => {
      if (cancelled) return
      if (document.visibilityState !== 'visible') return
      abort = new AbortController()
      try {
        const res = await pollNotificationChanges(epochRef.current, seqRef.current, 30, abort.signal)
        if (cancelled) return
        delayMs = 1000
        if (res.epoch) setEpoch(res.epoch)
        setSeq(res.seq)
        if (res.reload || res.seq > seqRef.current) {
          // epoch 不符或新写 → 刷新铃铛红点、通知中心列表与操作中心状态
          setTopicVersion((v) => v + 1)
          queryClient.invalidateQueries({ queryKey: ['notification-topics'] })
          // 操作执行产生通知/关闭 Topic 都会推进 GlobalSeq；一并刷新
          // 操作卡片（含上次执行摘要）、执行历史与已打开的执行详情抽屉
          // （UpdateRunState 同样推进 GlobalSeq，页面停留期间保持更新）。
          queryClient.invalidateQueries({ queryKey: ['operations'] })
          queryClient.invalidateQueries({ queryKey: ['operation-executions'] })
          queryClient.invalidateQueries({ queryKey: ['operation-execution'] })
        }
        clearTimer()
        timer = setTimeout(run, 0)
      } catch (err) {
        if (cancelled) return
        if (err instanceof Error && err.name === 'AbortError') return
        // 429 / 网络错误等 → 静默指数退避，红点保持最后已知值，不弹 toast
        clearTimer()
        timer = setTimeout(run, delayMs)
        delayMs = Math.min(delayMs * 2, 30_000)
      }
    }

    const handleVisibility = () => {
      if (document.hidden) {
        abort?.abort()
      } else {
        clearTimer()
        timer = setTimeout(run, 0)
      }
    }

    document.addEventListener('visibilitychange', handleVisibility)
    run()

    return () => {
      cancelled = true
      clearTimer()
      abort?.abort()
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  }, [queryClient])

  return { hasUnread }
}