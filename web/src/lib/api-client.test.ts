import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// 隔离 toast 副作用（sonner 在 node 下无 DOM），便于断言错误提示是否触发
vi.mock('@/components/ui/Toast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

import { ApiException, apiLongPoll, apiGet, apiPost, apiPut, apiDelete } from '@/lib/api-client'
import { toast } from '@/components/ui/Toast'

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

// ─────────────────────────────────────────────────────────────
// P3 主链路集成：apiGet/apiPost/apiPut/apiDelete 封装 + handleResponse 全分支
// 通过 stub global.fetch 模拟后端，覆盖 services-list（GET）+ CRUD（POST/PUT/DELETE）+ 长轮询错误分支
// ─────────────────────────────────────────────────────────────
describe('REST 封装（apiGet/apiPost/apiPut/apiDelete）', () => {
  it('apiGet：以 origin 为基址拼装 pathname+query，解析 data', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ data: [{ name: 'svc-a' }] }), { status: 200 }),
    )
    const r = await apiGet<{ data: Array<{ name: string }> }>('/api/services', {
      status: 'up',
      limit: 100,
      tag: undefined, // undefined 参数应被跳过
    })
    expect(r.data[0].name).toBe('svc-a')

    const [url, init] = vi.mocked(fetch).mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/services?status=up&limit=100')
    expect(init.method).toBe('GET')
    // 默认无 token → 含 Content-Type，但不含 Authorization
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect((init.headers as Record<string, string>)['Authorization']).toBeUndefined()
  })

  it('apiPost：带 JSON body 发送 POST', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ data: { id: 'run-1' } }), { status: 200 }),
    )
    const r = await apiPost<{ data: { id: string } }>('/api/extensions/x/run', {
      action: 'start',
    })
    expect(r.data.id).toBe('run-1')

    const [, init] = vi.mocked(fetch).mock.calls[0] as [unknown, RequestInit]
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ action: 'start' }))
  })

  it('apiPost：无 body 时不发送 body 字段', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('{}', { status: 200 }))
    await apiPost('/api/extensions/x/export')
    const [, init] = vi.mocked(fetch).mock.calls[0] as [unknown, RequestInit]
    expect(init.method).toBe('POST')
    expect(init.body).toBeUndefined()
  })

  it('apiPut：带 JSON body 发送 PUT', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('{}', { status: 200 }))
    await apiPut('/api/services/svc/config', { field: 'v' })
    const [, init] = vi.mocked(fetch).mock.calls[0] as [unknown, RequestInit]
    expect(init.method).toBe('PUT')
    expect(init.body).toBe(JSON.stringify({ field: 'v' }))
  })

  it('apiDelete：发送 DELETE', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('{}', { status: 200 }))
    await apiDelete('/api/services/svc')
    const [, init] = vi.mocked(fetch).mock.calls[0] as [unknown, RequestInit]
    expect(init.method).toBe('DELETE')
  })

  it('apiDelete：响应 204 时返回 undefined（handleResponse 204 分支）', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }))
    const r = await apiDelete('/api/services/svc')
    expect(r).toBeUndefined()
  })
})

describe('handleResponse 错误分支', () => {
  it('4xx 非 401：抛出 ApiException 并弹出 toast', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { code: 'SERVICE_NOT_FOUND', message: 'x' } }), {
        status: 404,
      }),
    )
    await expect(apiGet('/api/services/ghost')).rejects.toMatchObject({
      status: 404,
      code: 'SERVICE_NOT_FOUND',
    })
    expect(toast.error).toHaveBeenCalled()
  })

  it('5xx：抛出 ApiException 并弹出 toast', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { code: 'INTERNAL_ERROR', message: 'boom' } }), {
        status: 500,
      }),
    )
    await expect(apiGet('/api/services')).rejects.toMatchObject({ status: 500 })
    expect(toast.error).toHaveBeenCalled()
  })

  it('非标准错误体（无 error 字段）回退到整个 body 作为 ApiError', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ message: 'plain error' }), { status: 400 }),
    )
    await expect(apiGet('/api/x')).rejects.toMatchObject({
      status: 400,
      message: 'plain error',
    })
    expect(toast.error).toHaveBeenCalled()
  })

  it('silent=true：网络错误不弹出 toast', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    await expect(apiGet('/api/services', undefined, true)).rejects.toMatchObject({
      code: 'NETWORK_ERROR',
    })
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('silent=true：4xx 不弹出 toast', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ error: { code: 'SERVICE_NOT_FOUND', message: 'x' } }), {
        status: 404,
      }),
    )
    await expect(apiGet('/api/x', undefined, true)).rejects.toBeInstanceOf(ApiException)
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('POST 网络错误：TypeError 转换为 NETWORK_ERROR ApiException', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    await expect(apiPost('/api/extensions/x/run', { action: 'start' })).rejects.toMatchObject({
      code: 'NETWORK_ERROR',
    })
    expect(toast.error).toHaveBeenCalled()
  })

  it('POST AbortError：原样向上抛出', async () => {
    const abort = Object.assign(new Error('aborted'), { name: 'AbortError' })
    vi.mocked(fetch).mockRejectedValueOnce(abort)
    await expect(apiPost('/api/extensions/x/run', { action: 'start' })).rejects.toMatchObject({
      name: 'AbortError',
    })
  })
})
