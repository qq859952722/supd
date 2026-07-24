// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useHTTPProbe, sortAndLimitPorts, invalidatePortCache, type PortInfo } from '@/lib/http-probe'

// P3：http-probe 是 services-list 端口 HTTP 探测主路径。
// 用 jsdom + @testing-library/react 渲染 useHTTPProbe hook，stub global.fetch 模拟探测结果。
// 注意：probePort 仅判断 fetch resolve(HTTP) / reject(非HTTP)，不读取响应体，故 mock 返回占位对象即可。

const tcpPort = (port: number): PortInfo => ({
  protocol: 'tcp',
  port,
  address: `0.0.0.0:${port}`,
  state: 'LISTEN',
})
const udpPort = (port: number): PortInfo => ({
  protocol: 'udp',
  port,
  address: `0.0.0.0:${port}`,
  state: '',
})

const resolveHttp = () => vi.mocked(fetch).mockResolvedValue({} as unknown as Response)
const rejectHttp = () => vi.mocked(fetch).mockRejectedValue(new Error('not http'))

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
  invalidatePortCache() // 清空模块级缓存，避免跨测试污染
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('useHTTPProbe', () => {
  it('undefined 输入返回空数组', () => {
    const { result } = renderHook(() => useHTTPProbe(undefined))
    expect(result.current).toEqual([])
  })

  it('空端口列表返回空数组', () => {
    const { result } = renderHook(() => useHTTPProbe([]))
    expect(result.current).toEqual([])
  })

  it('非 tcp 端口不探测（fetch 不被调用）且 is_http 为 false', () => {
    resolveHttp()
    const { result } = renderHook(() => useHTTPProbe([udpPort(53)]))
    expect(result.current[0].is_http).toBe(false)
    expect(fetch).not.toHaveBeenCalled()
  })

  it('tcp 端口探测成功 → 异步更新 is_http 为 true', async () => {
    resolveHttp()
    const { result } = renderHook(() => useHTTPProbe([tcpPort(8080)]))
    // 初始渲染（探测前）为 false
    expect(result.current[0].is_http).toBe(false)
    await waitFor(() => expect(result.current[0].is_http).toBe(true))
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('探测失败（fetch reject）→ is_http 保持 false', async () => {
    rejectHttp()
    const { result } = renderHook(() => useHTTPProbe([tcpPort(9090)]))
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    // reject 不写入缓存，is_http 仍为 false
    expect(result.current[0].is_http).toBe(false)
  })

  it('缓存复用：相同端口集合仅探测一次', async () => {
    resolveHttp()
    const { result, rerender } = renderHook((ports: PortInfo[]) => useHTTPProbe(ports), {
      initialProps: [tcpPort(8080)],
    })
    await waitFor(() => expect(result.current[0].is_http).toBe(true))
    expect(fetch).toHaveBeenCalledTimes(1)
    // 相同 key 重新渲染 → 不应重复探测
    rerender([tcpPort(8080)])
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('invalidatePortCache 后，key 变化的同端口会被重新探测', async () => {
    resolveHttp()
    const { result, rerender } = renderHook((ports: PortInfo[]) => useHTTPProbe(ports), {
      initialProps: [tcpPort(8080)],
    })
    await waitFor(() => expect(result.current[0].is_http).toBe(true))
    expect(fetch).toHaveBeenCalledTimes(1)
    // 清除 8080 缓存，并以新端口集合触发 key 变化
    invalidatePortCache(8080)
    rerender([tcpPort(8080), tcpPort(9090)])
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3))
  })
})

describe('sortAndLimitPorts', () => {
  it('HTTP 端口优先，再按端口号升序，并限制数量', () => {
    const ports: PortInfo[] = [
      tcpPort(8080),
      { ...tcpPort(9090), is_http: true },
      { ...tcpPort(8081), is_http: true },
      tcpPort(80),
    ]
    const sorted = sortAndLimitPorts(ports, 2)
    expect(sorted).toHaveLength(2)
    expect(sorted[0].port).toBe(8081)
    expect(sorted[1].port).toBe(9090)
  })

  it('默认最多返回 3 个', () => {
    const ports: PortInfo[] = [8081, 8082, 8083, 8084].map((p) => ({
      ...tcpPort(p),
      is_http: true,
    }))
    expect(sortAndLimitPorts(ports)).toHaveLength(3)
  })
})
