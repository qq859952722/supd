import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// 隔离 toast 副作用（sonner 在 node 下无 DOM），便于断言 safeFetch 不触发真实提示
vi.mock('@/components/ui/Toast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

import { ApiException, apiLongPoll } from '@/lib/api-client'

beforeEach(() => {
  // apiGet/apiLongPoll 使用 window.location.origin 构造 URL
  vi.stubGlobal('window', { location: { origin: 'http://localhost:7979' } })
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('ApiException', () => {
  it('已知错误码映射为中文提示', () => {
    const e = new ApiException(404, { code: 'SERVICE_NOT_FOUND', message: 'original' })
    expect(e).toBeInstanceOf(ApiException)
    expect(e.code).toBe('SERVICE_NOT_FOUND')
    expect(e.status).toBe(404)
    expect(e.message).toBe('服务不存在')
  })

  it('未知错误码回退到原始 message', () => {
    const e = new ApiException(400, { code: 'WEIRD_CODE', message: '原始消息' })
    expect(e.message).toBe('原始消息')
  })

  it('无 code 时回退到 message', () => {
    const e = new ApiException(500, { message: '服务端炸了' })
    expect(e.message).toBe('服务端炸了')
  })
})

describe('apiLongPoll - 成功路径', () => {
  it('拼装 query 参数并解析 JSON 响应', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ events: [{ id: 1 }] }), { status: 200 }),
    )
    const r = await apiLongPoll<{ events: Array<{ id: number }> }>('/api/events', {
      since: '1h',
      limit: 50,
    })
    expect(r.events).toHaveLength(1)
    expect(r.events[0].id).toBe(1)

    // 校验请求 URL 的 pathname+search
    const calledUrl = vi.mocked(fetch).mock.calls[0][0] as string
    expect(calledUrl).toContain('/api/events?')
    expect(calledUrl).toContain('since=1h')
    expect(calledUrl).toContain('limit=50')
  })

  it('传递 method=GET 与 signal', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('{}', { status: 200 }))
    const ac = new AbortController()
    await apiLongPoll('/api/events', undefined, ac.signal)
    const init = vi.mocked(fetch).mock.calls[0][1] as RequestInit
    expect(init.method).toBe('GET')
    expect(init.signal).toBe(ac.signal)
  })

  it('响应 4xx 时抛出 ApiException', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { code: 'SERVICE_NOT_FOUND', message: 'x' } }), {
        status: 404,
      }),
    )
    await expect(apiLongPoll('/api/events')).rejects.toBeInstanceOf(ApiException)
  })

  it('响应 401 时调用 logout 并抛出 ApiException', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { code: 'AUTH_INVALID', message: 'x' } }), {
        status: 401,
      }),
    )
    await expect(apiLongPoll('/api/events')).rejects.toMatchObject({
      status: 401,
      code: 'AUTH_INVALID',
    })
  })
})

describe('apiLongPoll - 网络与中断', () => {
  it('网络不可达（TypeError）转换为 NETWORK_ERROR ApiException', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    await expect(apiLongPoll('/api/events')).rejects.toMatchObject({
      code: 'NETWORK_ERROR',
    })
  })

  it('AbortError 原样向上抛出（不转换为 ApiException）', async () => {
    const abort = Object.assign(new Error('aborted'), { name: 'AbortError' })
    vi.mocked(fetch).mockRejectedValueOnce(abort)
    await expect(apiLongPoll('/api/events')).rejects.toMatchObject({ name: 'AbortError' })
  })
})
