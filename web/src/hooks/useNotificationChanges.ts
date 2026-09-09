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
    setHasUnread(topicsData.topics.some((tm) => tm.last_seq > tm.read_seq))
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
          // epoch 不符或新写 → 刷新铃铛红点并推动通知中心列表刷新
          setTopicVersion((v) => v + 1)
          queryClient.invalidateQueries({ queryKey: ['notification-topics'] })
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