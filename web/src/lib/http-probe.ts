// HTTP 端口探测工具
// 通过浏览器 fetch 探测端口是否为 HTTP 服务
//
// 探测策略：
// fetch(mode:'no-cors') — HTTP 服务返回有效 HTTP 响应，浏览器创建 opaque 响应，fetch resolve
// 非 HTTP TCP 服务返回的数据无法被浏览器解析为 HTTP，fetch reject
// 端口不可达 → fetch reject

import { useState, useEffect, useRef } from 'react'

// 模块级缓存：port number → isHTTP（只缓存 true，不缓存 false）
const httpProbeCache = new Map<number, boolean>()
// 正在探测中的端口 Promise（避免并发重复探测）
const probingPromises = new Map<number, Promise<boolean>>()

// 探测超时（毫秒）
const PROBE_TIMEOUT = 2000

export interface PortInfo {
  protocol: string
  port: number
  address: string
  state: string
  is_http?: boolean
}

export interface ProbedPortInfo extends PortInfo {
  is_http: boolean
}

/**
 * 探测单个端口是否为 HTTP 服务
 *
 * 策略：
 * - fetch(mode:'no-cors') 探测
 * - resolve → HTTP 服务（浏览器收到了可解析的 HTTP 响应，创建 opaque 响应）
 * - reject → 非 HTTP TCP 或端口不可达
 */
function probePort(port: number): Promise<boolean> {
  // 只跳过已确认为 HTTP 的端口
  if (httpProbeCache.get(port) === true) {
    return Promise.resolve(true)
  }

  if (probingPromises.has(port)) {
    return probingPromises.get(port)!
  }

  const url = `http://127.0.0.1:${port}/`
  const promise = (async (): Promise<boolean> => {
    try {
      const controller = new AbortController()
      const timeoutId = setTimeout(() => controller.abort(), PROBE_TIMEOUT)

      await fetch(url, {
        method: 'GET',
        mode: 'no-cors',
        signal: controller.signal,
        cache: 'no-store',
      })

      clearTimeout(timeoutId)
      // fetch resolve → 浏览器收到了可解析的 HTTP 响应 → 是 HTTP 服务
      httpProbeCache.set(port, true)
      return true
    } catch {
      // fetch reject → 非 HTTP TCP 或端口不可达
      return false
    } finally {
      probingPromises.delete(port)
    }
  })()

  probingPromises.set(port, promise)
  return promise
}

/**
 * React Hook: 探测端口列表的 HTTP 状态
 * - 首次渲染立即返回缓存值（is_http 可能全为 false）
 * - 异步探测完成后自动触发重渲染，更新为探测结果
 * - 跨组件共享缓存，同一端口只探测一次
 * - 仅首次加载时探测，不循环
 */
export function useHTTPProbe(ports: PortInfo[] | undefined): ProbedPortInfo[] {
  const [, setVersion] = useState(0)
  const probedKeyRef = useRef('')

  // 生成端口列表的唯一 key
  const portsKey = ports
    ? ports.filter((p) => p.protocol.startsWith('tcp')).map((p) => p.port).sort().join(',')
    : ''

  useEffect(() => {
    if (!ports || ports.length === 0) return
    // 端口列表没变化且已探测过，跳过（不循环探测）
    if (portsKey === probedKeyRef.current) return

    // 找出需要探测的 TCP 端口（只跳过已确认为 HTTP 的端口）
    const toProbe = ports.filter(
      (p) => p.protocol.startsWith('tcp') && httpProbeCache.get(p.port) !== true
    )

    if (toProbe.length === 0) {
      // 全部已有缓存，标记当前 key 已探测，触发一次渲染
      probedKeyRef.current = portsKey
      setVersion((v) => v + 1)
      return
    }

    // 首次加载：并行探测所有未缓存的端口（非阻塞）
    probedKeyRef.current = portsKey
    Promise.all(toProbe.map((p) => probePort(p.port))).then(() => {
      setVersion((v) => v + 1)
    })
  }, [ports, portsKey])

  if (!ports || ports.length === 0) return []

  return ports.map((p) => ({
    ...p,
    is_http: p.protocol.startsWith('tcp') ? (httpProbeCache.get(p.port) ?? false) : false,
  }))
}

/**
 * 排序端口列表：HTTP 端口优先，然后按端口号升序
 * 并限制最大显示数量
 */
export function sortAndLimitPorts(ports: ProbedPortInfo[], maxCount: number = 3): ProbedPortInfo[] {
  const sorted = [...ports].sort((a, b) => {
    // HTTP 端口优先
    if (a.is_http !== b.is_http) return a.is_http ? -1 : 1
    // 同类按端口号升序
    return a.port - b.port
  })
  return sorted.slice(0, maxCount)
}

/**
 * 清除端口缓存（服务停止/重启时调用）
 */
export function invalidatePortCache(port?: number) {
  if (port !== undefined) {
    httpProbeCache.delete(port)
  } else {
    httpProbeCache.clear()
  }
}
