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
  // 长轮询静默模式：挂起期间服务重启/网络抖动/503 超限属预期场景，
  // 由调用方（轮询 hook）自行退避重试，不走全局 toast（否则形成错误风暴）。
  const response = await safeFetch(url.pathname + url.search, {
    method: 'GET',
    headers: buildHeaders(),
    signal,
  }, true)
  return handleResponse<T>(response, true)
}

// ---------------------------------------------------------------
// 节点 09：操作中心 + 通知中心 API 类型与方法
// 字段契约以后端真实 JSON 为准（snake_case，时间戳为 epoch 毫秒 number）。
// ---------------------------------------------------------------

/** store_error 可观测错误态（仅允许此对象或 null，不暴露底层细节）。 */
export interface StoreError {
  code: string
}

/** 操作卡片的上次执行摘要（§5 state 语义）。 */
export interface LastExecution {
  execution_id: string
  created_at: number
  state: 'running' | 'interrupted' | 'finished'
  result?: 'success' | 'failed'
}

export interface GlobalRef {
  extension_name: string
  action_id: string
}

/** 操作卡片（GET /api/operations 数组元素）。 */
export interface OperationCard {
  id: string
  label: string
  button_style: 'primary' | 'default' | 'danger'
  description: string
  registrants: string[]
  global_refs?: GlobalRef[]
  responder_count: number
  responders?: ResponderRef[]
  warnings: string[]
  last_execution?: LastExecution | null
}

/** operation 相应者引用（GET /api/operations/{id} 的 responders 元素）。 */
export interface ResponderRef {
  service_name: string
  extension_name: string
  action_id: string
}

/** 单操作详情。 */
export interface OperationDetail extends OperationCard {
  responders: ResponderRef[]
}

/** 操作执行中的 Run 快照（state 沿用七种任务状态）。 */
export interface OperationRun {
  run_id: string
  execution_id: string
  phase: 'global' | 'service'
  service_name: string | null
  extension_name: string
  action_id: string
  state: string
  started_at: number | null
  finished_at: number | null
}

/** 操作执行详情（GET /api/operation-executions 及 /{id}）。 */
export interface ExecutionDetail {
  id: string
  operation_id: string
  operation_label: string
  topic_id: string
  created_at: number
  finished_at: number | null
  interrupted_at: number | null
  runs: OperationRun[]
}

/** POST /api/operations/{id}/run 返回。 */
export interface RunOperationResult {
  execution_id: string
  topic_id: string
}

/** 操作中心（§十二.5.3）：卡片 / 详情 / 触发 / 执行历史。 */
export async function getOperations(): Promise<OperationCard[]> {
  return apiGet<OperationCard[]>('/api/operations')
}

export async function getOperation(id: string): Promise<OperationDetail> {
  return apiGet<OperationDetail>(`/api/operations/${encodeURIComponent(id)}`)
}

export async function runOperation(
  id: string,
  params: Record<string, unknown>,
  idempotencyKey: string,
): Promise<RunOperationResult> {
  const response = await safeFetch(`/api/operations/${encodeURIComponent(id)}/run`, {
    method: 'POST',
    headers: buildHeaders({ 'Idempotency-Key': idempotencyKey }),
    body: JSON.stringify({ params }),
  }, true)
  return handleResponse<RunOperationResult>(response, true)
}

/**
 * 执行历史（后端返回数组，非分页封装对象）。
 * 参数为服务端 limit/offset（1 起始不等价——直接透传）。
 */
export async function getOperationExecutions(limit?: number, offset?: number): Promise<ExecutionDetail[]> {
  return apiGet<ExecutionDetail[]>('/api/operation-executions', { limit, offset })
}

export async function getOperationExecution(id: string): Promise<ExecutionDetail> {
  return apiGet<ExecutionDetail>(`/api/operation-executions/${encodeURIComponent(id)}`)
}

/** 删除单条执行记录（run 级联；关联通知 Topic 不受影响）。 */
export async function deleteOperationExecution(id: string): Promise<{ ok: boolean }> {
  return apiDelete<{ ok: boolean }>(`/api/operation-executions/${encodeURIComponent(id)}`)
}

/** 清空全部执行记录。 */
export async function deleteAllOperationExecutions(): Promise<{ ok: boolean }> {
  return apiDelete<{ ok: boolean }>('/api/operation-executions')
}

/** 通知主题行（GET /api/notifications/topics 数组元素）。 */
export interface TopicItem {
  id: string
  kind: 'operation' | 'service' | 'extension' | 'system'
  source_name: string
  execution_id: string | null
  service_name: string | null
  created_at: number
  closed_at: number | null
  last_seq: number
  read_seq: number
  deleted_at: number | null
  // 最新通知摘要（Topic 无通知时为 null）
  last_level: string | null
  last_content: string | null
  last_source_type: string | null
  last_activity_at: number | null
}

/** 通知主题详情（含计数）。 */
export interface TopicDetail extends Omit<TopicItem, 'last_source_type'> {
  notification_count: number
  unread_count: number
  last_created_at: number | null
}

/** 单条不可变通知。 */
export interface Notification {
  id: string
  topic_id: string
  seq: number
  level: 'info' | 'success' | 'warning' | 'error'
  content: string
  created_at: number
  source_type: 'service' | 'extension' | 'system'
  service_name: string | null
  extension_name: string | null
  action_id: string | null
  run_id: string | null
  execution_id: string | null
}

export interface TopicListResponse {
  topics: TopicItem[]
  store_error: StoreError | null
}

export interface TopicDetailResponse {
  topic: TopicDetail | null
  notifications: Notification[]
  store_error: StoreError | null
  next_seq: number
  has_more: boolean
}

export interface ChangesResult {
  reload: boolean
  epoch: string
  seq: number
}

export interface NotificationTopicFilter {
  kind?: string
  unread?: boolean
  level?: string
  source_type?: string
  service_name?: string
}

/** 通知中心（§八）：topics / topic / read / read-all / delete / clear / changes。 */
export async function getNotificationTopics(filter: NotificationTopicFilter = {}): Promise<TopicListResponse> {
  return apiGet<TopicListResponse>('/api/notifications/topics', {
    kind: filter.kind,
    unread: filter.unread,
    level: filter.level,
    source_type: filter.source_type,
    service_name: filter.service_name,
  })
}

/** sinceSeq 为已返回的最大 seq；服务端返回 seq > sinceSeq 的通知（seq 参数）。 */
export async function getNotificationTopic(id: string, sinceSeq?: number, limit?: number): Promise<TopicDetailResponse> {
  return apiGet<TopicDetailResponse>(`/api/notifications/topics/${encodeURIComponent(id)}`, { seq: sinceSeq, limit })
}

export async function markTopicRead(id: string): Promise<{ read_seq: number }> {
  return apiPost<{ read_seq: number }>(`/api/notifications/topics/${encodeURIComponent(id)}/read`)
}

export async function markAllRead(): Promise<{ ok: boolean }> {
  return apiPost<{ ok: boolean }>('/api/notifications/read-all')
}

export async function deleteTopic(id: string): Promise<{ ok: boolean }> {
  return apiDelete<{ ok: boolean }>(`/api/notifications/topics/${encodeURIComponent(id)}`)
}

export async function clearAllTopics(): Promise<{ ok: boolean }> {
  return apiDelete<{ ok: boolean }>('/api/notifications/topics')
}

export async function pollNotificationChanges(epoch: string, sinceSeq: number, wait: number, signal?: AbortSignal): Promise<ChangesResult> {
  return apiLongPoll<ChangesResult>('/api/notifications/changes', {
    epoch: epoch || undefined,
    since: sinceSeq,
    wait,
  }, signal)
}
