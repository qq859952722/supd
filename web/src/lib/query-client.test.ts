import { describe, it, expect, vi, beforeEach } from 'vitest'

// 隔离 toast 副作用（api-client 间接依赖 Toast/sonner）
vi.mock('@/components/ui/Toast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

// 捕获 QueryClient 构造参数以直接断言 retry 回调的分支逻辑
let capturedOptions: any = null
vi.mock('@tanstack/react-query', async () => {
  const actual = await vi.importActual<typeof import('@tanstack/react-query')>(
    '@tanstack/react-query',
  )
  return {
    ...actual,
    QueryClient: class extends actual.QueryClient {
      constructor(opts?: any) {
        super(opts)
        capturedOptions = opts
      }
    },
  }
})

import { QueryClient } from '@tanstack/react-query'
import { createQueryClient } from '@/lib/query-client'
import { ApiException } from '@/lib/api-client'

beforeEach(() => {
  capturedOptions = null
})

describe('createQueryClient', () => {
  it('返回 QueryClient 实例', () => {
    const qc = createQueryClient()
    expect(qc).toBeInstanceOf(QueryClient)
  })

  it('retry 回调：401 不重试', () => {
    createQueryClient()
    const retry = capturedOptions.defaultOptions.queries.retry as (
      n: number,
      e: unknown,
    ) => boolean
    expect(retry(0, new ApiException(401, { code: 'AUTH_INVALID', message: 'x' }))).toBe(false)
  })

  it('retry 回调：非 401 且失败次数 < 3 时重试', () => {
    createQueryClient()
    const retry = capturedOptions.defaultOptions.queries.retry as (
      n: number,
      e: unknown,
    ) => boolean
    expect(retry(0, new ApiException(500, { message: 'x' }))).toBe(true)
    expect(retry(2, new ApiException(500, { message: 'x' }))).toBe(true)
  })

  it('retry 回调：失败次数达到 3 次停止重试', () => {
    createQueryClient()
    const retry = capturedOptions.defaultOptions.queries.retry as (
      n: number,
      e: unknown,
    ) => boolean
    expect(retry(3, new ApiException(500, { message: 'x' }))).toBe(false)
  })

  it('mutation 默认不重试', () => {
    createQueryClient()
    expect(capturedOptions.defaultOptions.mutations.retry).toBe(false)
  })
})
