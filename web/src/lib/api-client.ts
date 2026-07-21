// REQ-I-001~005: API客户端封装
// RESTful资源导向、JSON响应、Bearer token认证、统一错误处理

import { useAuthStore } from '@/stores/auth'
import { toast } from '@/components/ui/Toast'

/** API错误结构 (REQ-I-005: code/message/details) */
export interface ApiError {
  code: string
  message: string
  details?: Record<string, unknown>
}

/** REQ-D-009: 错误码 → 中文提示映射表 */
const errorCodeMessages: Record<string, string> = {
  AUTH_REQUIRED: '认证失败：未提供有效的 Token',
  AUTH_INVALID: '认证失败：Token 无效或已失效',
  SERVICE_NOT_FOUND: '服务不存在',
  SERVICE_EXISTS: '服务已存在',
  SERVICE_RUNNING: '服务运行中，无法执行此操作',
  SERVICE_BUSY: '请求并发超限，请稍后重试',
  DEPENDENCY_CYCLE: '服务依赖存在循环引用',
  DEPENDENCY_MISSING: '依赖服务缺失',
  SERVICE_CONFIG_INVALID: '服务配置校验失败',
  RUNTIME_NOT_FOUND: '运行时未找到',
  RUNTIME_NOT_EXECUTABLE: '运行时路径不可执行',
  RUNTIME_USER_NOT_FOUND: '运行时指定的用户不存在',
  EXTENSION_NOT_FOUND: '扩展不存在',
  EXTENSION_FAILED: '扩展运行失败',
  RUN_NOT_FOUND: '任务不存在',
  RUN_ALREADY_DONE: '任务已完成，无法取消',
  FILE_NOT_FOUND: '文件不存在',
  FILE_PERMISSION: '文件权限不足',
  FILE_TOO_LARGE: '文件大小超过上传限制',
  FILE_ACCESS_DENIED: '文件访问被拒绝',
  INVALID_REQUEST: '请求参数错误',
  INTERNAL_ERROR: '服务器内部错误，请查看服务端日志',
  NOT_FOUND: '接口不存在',
  METHOD_NOT_ALLOWED: '请求方法不被允许',
  // P-01-03: fetch 网络错误（supd 不可达）
  NETWORK_ERROR: '网络连接失败，请检查 supd 服务是否运行',
}

/** 根据错误码获取中文提示，未匹配时返回 undefined */
function getLocalizedErrorMessage(code: string): string | undefined {
  return errorCodeMessages[code]
}

export class ApiException extends Error {
  code: string
  details?: Record<string, unknown>
  status: number

  constructor(status: number, error: ApiError) {
    // P-01-01: 优先使用错误码对应的中文提示，无映射时回退到原始 message
    const localized = error.code ? getLocalizedErrorMessage(error.code) : undefined
    super(localized ?? error.message)
    this.name = 'ApiException'
    this.code = error.code
    this.details = error.details
    this.status = status
  }
}

/** 通用API响应结构 */
export interface ApiResponse<T> {
  data: T
}

function getToken(): string | null {
  return useAuthStore.getState().token
}

function buildHeaders(custom?: HeadersInit): HeadersInit {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  const token = getToken()
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }
  if (custom instanceof Headers) {
    custom.forEach((value, key) => { headers[key] = value })
  } else if (custom) {
    Object.entries(custom).forEach(([key, value]) => {
      if (typeof value === 'string') headers[key] = value
    })
  }
  return headers
}

/**
 * P-01-03: 包装 fetch，捕获网络错误（TypeError: Failed to fetch）并转换为中文 ApiException。
 * 当 supd 后端不可达时，浏览器 fetch 抛出英文 TypeError，此处统一转换为 NETWORK_ERROR。
 */
async function safeFetch(input: RequestInfo | URL, init?: RequestInit, silent = false): Promise<Response> {
  try {
    return await fetch(input, init)
  } catch (err) {
    // E-02-001: 用户主动取消的请求（AbortError）不视为网络错误，向上抛出由调用方处理
    if (err instanceof Error && err.name === 'AbortError') {
      throw err
    }
    // 网络断开时 fetch 抛 TypeError("Failed to fetch")，统一转换为中文 NETWORK_ERROR
    const apiError = new ApiException(0, {
      code: 'NETWORK_ERROR',
      message: '网络连接失败，请检查 supd 服务是否运行',
    })
    if (!silent) {
      toast.error(apiError.message)
    }
    throw apiError
  }
}

async function handleResponse<T>(response: Response, silent = false): Promise<T> {
  if (response.status === 204) {
    return undefined as T
  }

  const body = await response.json()

  if (!response.ok) {
    if (response.status === 401) {
      // token失效，清除认证状态
      useAuthStore.getState().logout()
    }
    // P-01-01: 后端错误响应格式为 {"error": {"code":..., "message":..., "details":...}}
    // 兼容直接返回错误对象的场景
    const errorObj: ApiError = (body && typeof body === 'object' && 'error' in body && body.error && typeof body.error === 'object')
      ? body.error as ApiError
      : body as ApiError
    const err = new ApiException(response.status, errorObj)
    // 全局错误提示：非silent且非401错误展示 toast
    if (!silent && response.status !== 401) {
      toast.error(err.message || `请求失败 (${response.status})`)
    }
    throw err
  }

  return body as T
}

/** REQ-I-001: GET请求 */
export async function apiGet<T>(
  path: string,
  params?: Record<string, string | number | boolean | undefined>,
  silent = false,
): Promise<T> {
  const url = new URL(path, window.location.origin)
  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined) {
        url.searchParams.set(key, String(value))
      }
    })
  }
  const response = await safeFetch(url.pathname + url.search, {
    method: 'GET',
    headers: buildHeaders(),
  }, silent)
  return handleResponse<T>(response, silent)
}

/** REQ-I-002: POST请求（创建资源） */
export async function apiPost<T>(path: string, body?: unknown, silent = false): Promise<T> {
  const response = await safeFetch(path, {
    method: 'POST',
    headers: buildHeaders(),
    body: body !== undefined ? JSON.stringify(body) : undefined,
  }, silent)
  return handleResponse<T>(response, silent)
}

/** REQ-I-003: PUT请求（更新资源） */
export async function apiPut<T>(path: string, body?: unknown, silent = false): Promise<T> {
  const response = await safeFetch(path, {
    method: 'PUT',
    headers: buildHeaders(),
    body: body !== undefined ? JSON.stringify(body) : undefined,
  }, silent)
  return handleResponse<T>(response, silent)
}

/** REQ-I-004: DELETE请求 */
export async function apiDelete<T>(path: string, silent = false): Promise<T> {
  const response = await safeFetch(path, {
    method: 'DELETE',
    headers: buildHeaders(),
  }, silent)
  return handleResponse<T>(response, silent)
}

/** 长轮询专用GET (REQ-I-001: 30秒挂起) */
export async function apiLongPoll<T>(path: string, params?: Record<string, string | number | boolean | undefined>, signal?: AbortSignal): Promise<T> {
  const url = new URL(path, window.location.origin)
  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined) {
        url.searchParams.set(key, String(value))
      }
    })
  }
  const response = await safeFetch(url.pathname + url.search, {
    method: 'GET',
    headers: buildHeaders(),
    signal,
  })
  return handleResponse<T>(response)
}
