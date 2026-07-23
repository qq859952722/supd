// REQ-U-005, REQ-U-009: 服务详情页 — 7个标签页
// 概览/日志/进程/配置/环境变量/扩展/历史
// 11种边界操作 (REQ-F-040)

import { useState, useMemo } from 'react'
import { useParams, Link, useSearchParams, useNavigate } from 'react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost, apiPut, apiDelete } from '@/lib/api-client'
import { t } from '@/lib/i18n'
import type { ServiceState } from '@/components/dashboard/ServiceOverview'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/Tabs'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { LogViewer } from '@/components/service/LogViewer'
import { ProcessTree } from '@/components/service/ProcessTree'
import { EnvEditor } from '@/components/service/EnvEditor'
import { parseEnvYaml } from '@/lib/env-yaml'
import { MonacoEditor } from '@/components/editor/MonacoEditor'
import { ServiceForm, serializeServiceConfig, parseServiceYaml, type ServiceConfig } from '@/components/service/ServiceForm'
import { IconRenderer } from '@/components/common/IconRenderer'
import { DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/Dialog'
import { SkeletonCard } from '@/components/ui/Skeleton'
import { toast } from '@/components/ui/Toast'
import { useTaskToast } from '@/components/ui/TaskToast'
import { useHTTPProbe, type ProbedPortInfo } from '@/lib/http-probe'
import { getErrorMessage } from '@/lib/error-utils'
import {
  ArrowLeft,
  Play,
  Square,
  RotateCw,
  Zap,
  XCircle,
  AlertTriangle,
  FileText,
  Download,
  Trash2,
  Terminal,
  Settings,
  Clock,
  Server,
  Puzzle,
  Cpu,
  MemoryStick,
  HardDrive,
  Network,
  ExternalLink,
  Loader2,
  X,
  Eraser,
  Plus,
  Pencil,
  ToggleRight,
  ToggleLeft,
} from 'lucide-react'

// 类型定义
interface ServiceConfigDetail {
  name: string
  version: string
  description?: string
  icon?: string
  autostart?: boolean
  command: string[]
  runtime?: string
  user?: string
  group?: string
  workdir?: string
  depends_on?: string[]
  tags?: string[]
}

interface ServiceDetailData {
  name: string
  status: ServiceState
  pid: number
  uptime: number
  restart_count: number
  config?: ServiceConfigDetail
  config_path?: string
  pending_changes?: string[]
  cpu_percent?: number
  memory_mb?: number
}

// REQ-I-006: 历史记录条目（与后端 HistoryEntry 对应）
interface HistoryEntry {
  time: string
  pid: number
  exit_code: number
  duration_seconds: number
  reason: string
}

// REQ-I-006: 服务资源使用响应（与后端 ResourceResponse 对应）
interface ServiceResourceInfo {
  cpu_percent: number
  memory_mb: number
  memory_percent: number
  process_count: number
  fd_count: number
  disk_total_mb?: number
  disk_used_mb?: number
  ports?: PortInfo[]
}

// PortInfo 进程监听端口信息（与后端 PortInfo 对应）
interface PortInfo {
  protocol: string // tcp / tcp6 / udp / udp6
  port: number
  address: string // 绑定地址，如 0.0.0.0 / 127.0.0.1 / ::
  state: string // TCP状态：LISTEN 等；UDP固定为 ""
  is_http?: boolean // 由前端浏览器探测判定
}

interface HistoryResponse {
  entries: HistoryEntry[]
}

// 服务级扩展摘要（与后端 ExtensionSummary 对应）
interface ServiceExtensionSummary {
  name: string
  version?: string
  description?: string
  enabled: boolean
  display_state?: string
  trigger_type?: string
  service?: string
  run_count?: number
  success_count?: number
  fail_count?: number
  last_run_at?: string
  last_status?: string
  // 完整触发器配置
  triggers?: {
    on_demand?: boolean
    on_schedule?: Array<{ cron: string; action: string }>
    service_lifecycle?: Array<{ event: string; action: string }>
    supd_lifecycle?: Array<{ event: string; action: string }>
  }
  // 动作列表
  actions?: Array<{ id: string; label?: string; button_style?: string; args?: string[] }>
  // 并发策略
  concurrency?: string
}

// E-09-001: 扩展创建/编辑可视化表单数据
interface ExtFormData {
  name: string
  version: string
  description: string
  runtime: string
  entry: string
  timeout_seconds: number
  identityMode: 'user' | 'uid'
  run_as: string
  run_as_uid: string
  run_as_gid: string
  run_as_groups: string
  concurrency: string
  triggers_on_demand: boolean
}

// 扩展运行历史条目（与后端 RunRecord 对应）
interface ExtRunHistory {
  run_id: string
  extension_name: string
  action_id?: string
  service_name?: string
  state: string
  exit_code?: number
  progress?: number
  result_msg?: string
  result_level?: string
  started_at: string
  finished_at?: string
  trigger_type?: string
}

const stateVariantMap: Record<ServiceState, 'default' | 'info' | 'success' | 'warning' | 'danger' | 'secondary'> = {
  pending: 'secondary',
  starting: 'info',
  up: 'success',
  ready: 'success',
  stopping: 'warning',
  down: 'secondary',
  failed: 'danger',
}

function formatUptime(seconds: number): string {
  if (seconds <= 0) return '-'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}天${h}时${m}分`
  if (h > 0) return `${h}时${m}分`
  return `${m}分`
}

function formatMB(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb.toFixed(1)} MB`
}

export function ServiceDetail() {
  const { name } = useParams<{ name: string }>()
  const serviceName = name ?? ''
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { runExtension: runExtensionToast } = useTaskToast()
  // 读取 URL 查询参数 ?tab=logs 以支持从服务列表「日志」按钮直接跳转到日志页签
  const [searchParams] = useSearchParams()
  const initialTab = searchParams.get('tab') || 'overview'
  const [activeTab, setActiveTab] = useState(initialTab)
  const [configContent, setConfigContent] = useState('')
  const [configInitialized, setConfigInitialized] = useState(false)
  const [configEditMode, setConfigEditMode] = useState<'visual' | 'yaml'>('yaml')
  const [signalInput, setSignalInput] = useState('SIGTERM')
  const [showSignalDialog, setShowSignalDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [showForceStopDialog, setShowForceStopDialog] = useState(false)
  // 扩展日志对话框状态 — 统一对话框（左侧运行列表 + 右侧日志内容）
  const [extLogExt, setExtLogExt] = useState<string | null>(null)
  const [extLogSelectedRunId, setExtLogSelectedRunId] = useState<string | null>(null)
  // E-09-001: 服务级扩展 CRUD — 创建/编辑表单对话框状态
  const [showExtFormDialog, setShowExtFormDialog] = useState(false)
  const [editingExtName, setEditingExtName] = useState<string | null>(null)
  const [extForm, setExtForm] = useState<ExtFormData | null>(null)
  // 编辑扩展时保存原有完整配置（防止完整更新丢失 triggers/actions/ui 等字段）
  const [extFormOriginal, setExtFormOriginal] = useState<Record<string, unknown> | null>(null)
  const [showExtDeleteDialog, setShowExtDeleteDialog] = useState<string | null>(null)

  // E-01-002 修复：区分加载态与错误态，避免404时永远显示 skeleton
  const { data: service, isLoading, isError } = useQuery({
    queryKey: ['service-detail', serviceName],
    queryFn: () => apiGet<ServiceDetailData>(`/api/services/${encodeURIComponent(serviceName)}`),
    refetchInterval: 2_000, // REQ-2.9.11: 短轮询 2s
  })

  // REQ-I-006: 服务资源占用（CPU/内存/磁盘分区）
  const { data: resources } = useQuery({
    queryKey: ['service-resources', serviceName],
    queryFn: () => apiGet<ServiceResourceInfo>(`/api/services/${encodeURIComponent(serviceName)}/resources`),
    refetchInterval: 5_000,
    enabled: !!serviceName && !!service?.pid,
  })

  // 获取原始 YAML 配置文件内容
  const { data: yamlContent } = useQuery({
    queryKey: ['service-config-yaml', serviceName, service?.config_path],
    queryFn: async () => {
      if (!service?.config_path) return null
      const res = await apiGet<{ path: string; content: string }>(`/api/files`, { path: service.config_path })
      return res.content
    },
    enabled: !!service?.config_path,
  })

  // REQ-I-006: 获取服务启动历史
  const { data: historyData } = useQuery({
    queryKey: ['service-history', serviceName],
    queryFn: () => apiGet<HistoryResponse>(`/api/services/${encodeURIComponent(serviceName)}/history`),
    enabled: !!serviceName,
  })

  // REQ-I-006: 获取服务死亡历史
  const { data: deathsData } = useQuery({
    queryKey: ['service-deaths', serviceName],
    queryFn: () => apiGet<HistoryResponse>(`/api/services/${encodeURIComponent(serviceName)}/deaths`),
    enabled: !!serviceName,
  })

  // 获取服务级 env.yaml（通过统一文件接口，silent 避免不存在时弹 toast）
  const { data: serviceEnvContent } = useQuery({
    queryKey: ['service-env-file', serviceName],
    queryFn: async () => {
      try {
        const res = await apiGet<{ path: string; content: string }>(
          '/api/files',
          { path: `services/${serviceName}/env.yaml` },
          true,
        )
        return res.content
      } catch {
        return ''
      }
    },
    enabled: !!serviceName,
    retry: false,
  })

  // 获取全局环境变量（作为继承来源，silent 避免未配置时弹 toast）
  const { data: globalEnv } = useQuery({
    queryKey: ['global-env'],
    queryFn: async () => {
      try {
        return await apiGet<{ env: Record<string, { value: string; enabled?: boolean; hint?: string }> }>('/api/settings/env', undefined, true)
      } catch {
        return { env: {} }
      }
    },
    enabled: !!serviceName,
    retry: false,
  })

  // 获取服务级扩展列表
  const { data: serviceExtensions } = useQuery({
    queryKey: ['service-extensions', serviceName],
    queryFn: async () => {
      try {
        return await apiGet<ServiceExtensionSummary[]>(
          `/api/services/${encodeURIComponent(serviceName)}/extensions`,
          undefined,
          true,
        )
      } catch {
        return []
      }
    },
    enabled: !!serviceName,
    retry: false,
  })

  // 同步配置内容：优先使用原始 YAML，否则 fallback 到 JSON
  if (!configInitialized) {
    if (yamlContent) {
      setConfigContent(yamlContent)
      setConfigInitialized(true)
    } else if (service?.config && !service?.config_path) {
      setConfigContent(JSON.stringify(service.config, null, 2))
      setConfigInitialized(true)
    }
  }

  // 操作mutations — E-02 修复：silent=true 避免重复 toast，提取后端错误消息
  const startMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(serviceName)}/start`, undefined, true),
    onSuccess: () => { toast.success(`${serviceName} 启动指令已发送`); queryClient.invalidateQueries({ queryKey: ['service-detail', serviceName] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '启动失败')) },
  })

  const stopMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(serviceName)}/stop`, undefined, true),
    onSuccess: () => { toast.success(`${serviceName} 停止指令已发送`); queryClient.invalidateQueries({ queryKey: ['service-detail', serviceName] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '停止失败')) },
  })

  const restartMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(serviceName)}/restart`, undefined, true),
    onSuccess: () => { toast.success(`${serviceName} 重启指令已发送`); queryClient.invalidateQueries({ queryKey: ['service-detail', serviceName] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '重启失败')) },
  })

  const forceStopMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(serviceName)}/force-stop`, undefined, true),
    onSuccess: () => { toast.success(`${serviceName} 强制停止指令已发送`); queryClient.invalidateQueries({ queryKey: ['service-detail', serviceName] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '强制停止失败')) },
  })

  const signalMutation = useMutation({
    mutationFn: (signal: string) => apiPost(`/api/services/${encodeURIComponent(serviceName)}/signal`, { signal }, true),
    onSuccess: () => { toast.success(`信号已发送`); setShowSignalDialog(false) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '发送信号失败')) },
  })

  const clearFailedMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(serviceName)}/clear-failed`, undefined, true),
    onSuccess: () => { toast.success('已清除失败状态'); queryClient.invalidateQueries({ queryKey: ['service-detail', serviceName] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '清除失败')) },
  })

  // 运行时列表（用于扩展表单的运行时选择）
  const { data: runtimesData } = useQuery({
    queryKey: ['runtimes'],
    queryFn: () => apiGet<{ runtimes: Array<{ alias: string; available: boolean; source: string }> }>('/api/runtimes'),
  })
  const runtimeOptions = useMemo(() => {
    const opts = [{ value: '', label: '无（直接执行）' }]
    const seen = new Set<string>()
    if (runtimesData?.runtimes) {
      for (const rt of runtimesData.runtimes) {
        if (rt.available && !seen.has(rt.alias)) {
          seen.add(rt.alias)
          opts.push({ value: rt.alias, label: rt.alias })
        }
      }
    }
    // 兜底：API 无结果时显示内置运行时
    if (opts.length === 1) {
      for (const r of ['bash', 'sh', 'python3', 'node']) {
        opts.push({ value: r, label: r })
      }
    }
    // 当前值不在列表中时追加
    if (extForm?.runtime && !seen.has(extForm.runtime)) {
      opts.push({ value: extForm.runtime, label: extForm.runtime })
    }
    return opts
  }, [runtimesData, extForm?.runtime])

  // E-09-001: 服务级扩展 CRUD mutations
  // 创建扩展：POST /api/services/{name}/extensions，body 为 ExtensionMeta JSON
  const createExtMutation = useMutation({
    mutationFn: (meta: Record<string, unknown>) =>
      apiPost(`/api/services/${encodeURIComponent(serviceName)}/extensions`, meta, true),
    onSuccess: () => {
      toast.success('扩展已创建')
      setShowExtFormDialog(false)
      queryClient.invalidateQueries({ queryKey: ['service-extensions', serviceName] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '创建扩展失败')) },
  })
  // 更新扩展：PUT /api/services/{name}/extensions/{ext}
  const updateExtMutation = useMutation({
    mutationFn: ({ extName, meta }: { extName: string; meta: Record<string, unknown> }) =>
      apiPut(`/api/services/${encodeURIComponent(serviceName)}/extensions/${encodeURIComponent(extName)}`, meta, true),
    onSuccess: () => {
      toast.success('扩展已更新')
      setShowExtFormDialog(false)
      queryClient.invalidateQueries({ queryKey: ['service-extensions', serviceName] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '更新扩展失败')) },
  })
  // 删除扩展：DELETE /api/services/{name}/extensions/{ext}
  const deleteExtMutation = useMutation({
    mutationFn: (extName: string) =>
      apiDelete(`/api/services/${encodeURIComponent(serviceName)}/extensions/${encodeURIComponent(extName)}`, true),
    onSuccess: () => {
      toast.success('扩展已删除')
      setShowExtDeleteDialog(null)
      queryClient.invalidateQueries({ queryKey: ['service-extensions', serviceName] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '删除扩展失败')) },
  })

  // 打开创建扩展对话框 — 初始化表单默认值
  const handleCreateExtension = () => {
    setEditingExtName(null)
    setExtFormOriginal(null)
    setExtForm({
      name: '',
      version: '1.0.0',
      description: '',
      runtime: 'bash',
      entry: 'run.sh',
      timeout_seconds: 600,
      identityMode: 'user',
      run_as: '',
      run_as_uid: '',
      run_as_gid: '',
      run_as_groups: '',
      concurrency: 'replace',
      triggers_on_demand: true,
    })
    setShowExtFormDialog(true)
  }
  // 打开编辑扩展对话框 — 从 API 返回的配置回填表单
  const handleEditExtension = async (extName: string) => {
    setEditingExtName(extName)
    try {
      const detail = await apiGet<{ config?: Record<string, unknown> }>(
        `/api/services/${encodeURIComponent(serviceName)}/extensions/${encodeURIComponent(extName)}`,
        undefined, true,
      )
      const c = detail.config ?? {}
      setExtFormOriginal(c)
      const triggers = c.triggers as { on_demand?: boolean } | undefined
      setExtForm({
        name: String(c.name ?? extName),
        version: String(c.version ?? '1.0.0'),
        description: String(c.description ?? ''),
        runtime: String(c.runtime ?? 'bash'),
        entry: String(c.entry ?? 'run.sh'),
        timeout_seconds: Number(c.timeout_seconds) || 600,
        identityMode: c.run_as_uid ? 'uid' : 'user',
        run_as: String(c.run_as ?? ''),
        run_as_uid: c.run_as_uid != null ? String(c.run_as_uid) : '',
        run_as_gid: c.run_as_gid != null ? String(c.run_as_gid) : '',
        run_as_groups: Array.isArray(c.run_as_groups) ? (c.run_as_groups as number[]).join(', ') : '',
        concurrency: String(c.concurrency ?? 'replace'),
        triggers_on_demand: triggers?.on_demand !== false,
      })
    } catch (err) {
      toast.error(getErrorMessage(err, '加载扩展配置失败'))
      setExtForm(null)
      return
    }
    setShowExtFormDialog(true)
  }
  // 保存扩展（创建或更新）— 从表单对象构建 API body
  const handleSaveExtension = () => {
    if (!extForm) return
    if (!extForm.name.trim()) { toast.error('扩展名称不能为空'); return }
    const meta: Record<string, unknown> = {
      name: extForm.name,
      version: extForm.version,
      description: extForm.description,
      runtime: extForm.runtime,
      entry: extForm.entry,
      timeout_seconds: extForm.timeout_seconds,
      concurrency: extForm.concurrency,
    }
    // §2.2.13: run_as（User 模式）与 run_as_uid（UID 模式）互斥，由 identityMode 决定
    if (extForm.identityMode === 'uid') {
      const uid = parseInt(extForm.run_as_uid, 10)
      if (!isNaN(uid) && uid > 0) meta.run_as_uid = uid
      const gid = parseInt(extForm.run_as_gid, 10)
      if (!isNaN(gid) && gid > 0) meta.run_as_gid = gid
      const groupsNums = extForm.run_as_groups.split(',').map((s) => s.trim()).filter(Boolean).map(Number).filter((n) => !isNaN(n) && n > 0)
      if (groupsNums.length) meta.run_as_groups = groupsNums
    } else {
      if (extForm.run_as) meta.run_as = extForm.run_as
    }
    if (editingExtName && extFormOriginal) {
      // 编辑模式：以原有配置为基础，仅更新表单编辑的字段
      // 保留原有的 triggers/actions/ui/enabled 等配置不被覆盖
      meta.triggers = extFormOriginal.triggers ?? { on_demand: extForm.triggers_on_demand }
      if (extFormOriginal.actions) meta.actions = extFormOriginal.actions
      if (extFormOriginal.ui) meta.ui = extFormOriginal.ui
      if (extFormOriginal.enabled !== undefined) meta.enabled = extFormOriginal.enabled
      updateExtMutation.mutate({ extName: editingExtName, meta })
    } else {
      // 创建模式：只设置 on_demand 触发器
      meta.triggers = { on_demand: extForm.triggers_on_demand }
      createExtMutation.mutate(meta)
    }
  }

  // REQ-2.11.4: 统一通过 PUT /api/files 保存文件
  const saveConfigMutation = useMutation({
    mutationFn: (content: string) => {
      const configPath = service?.config_path ?? `services/${serviceName}/service.yaml`
      return apiPut('/api/files?path=' + encodeURIComponent(configPath), { content }, true)
    },
    onSuccess: () => { toast.success('配置已保存'); queryClient.invalidateQueries({ queryKey: ['service-detail', serviceName] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '保存配置失败')) },
  })

  // 将配置内容解析为 ServiceConfig 对象，用于可视化编辑表单的初始值
  const parsedConfig = useMemo<Partial<ServiceConfig> | undefined>(() => {
    if (!configContent) return undefined
    // JSON 回退（无 config_path 时 configContent 可能为 JSON）
    if (configContent.trim().startsWith('{')) {
      try {
        return JSON.parse(configContent) as Partial<ServiceConfig>
      } catch {
        // 降级到 YAML 解析
      }
    }
    return parseServiceYaml(configContent)
  }, [configContent])

  // 可视化表单提交：序列化为 YAML 后保存
  const handleVisualSave = (config: ServiceConfig) => {
    const yaml = serializeServiceConfig(config)
    setConfigContent(yaml)
    saveConfigMutation.mutate(yaml)
  }

  const deleteMutation = useMutation({
    mutationFn: () => apiDelete(`/api/services/${encodeURIComponent(serviceName)}`, true),
    onSuccess: () => {
      toast.success('服务已删除')
      setShowDeleteDialog(false)
      // 刷新服务列表缓存并导航回列表页
      queryClient.invalidateQueries({ queryKey: ['services-list'] })
      navigate('/services')
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '删除失败')) },
  })

  // 运行服务级扩展 — 非阻塞式浮动通知
  // 点击动作按钮后，在右下角显示浮动进度卡片，不独占窗口
  // POST 是同步的（等待扩展完成），期间通过轮询 runs 获取实时进度
  // 触发扩展运行 — 非阻塞浮动通知（不打开独占对话框）
  const runExtensionWithLog = (extName: string, action?: string) => {
    runExtensionToast({ extensionName: extName, action, serviceName })
  }

  // 获取指定扩展的运行历史 — 统一日志对话框使用
  // 打开对话框时加载，运行中时2秒轮询刷新进度
  const { data: extRunsData, isLoading: extRunsLoading } = useQuery({
    queryKey: ['service-ext-runs', serviceName, extLogExt],
    queryFn: () =>
      apiGet<ExtRunHistory[]>(
        '/api/extensions/runs',
        { service_name: serviceName, extension_name: extLogExt ?? undefined, limit: 50 },
      ),
    enabled: !!extLogExt,
    refetchInterval: (query) => {
      const runs = query.state.data
      if (Array.isArray(runs) && runs.some((r) => r.state === 'running' || r.state === 'pending')) {
        return 2000
      }
      return false
    },
  })

  // 统一日志对话框：运行列表
  const extRuns = Array.isArray(extRunsData) ? extRunsData : []
  // 当前选中的 run（默认选中最新一条）
  const extSelectedRun = extLogSelectedRunId
    ? extRuns.find((r) => r.run_id === extLogSelectedRunId) ?? null
    : extRuns[0] ?? null
  const extSelectedRunId = extSelectedRun?.run_id ?? null

  // 获取选中 run 的日志内容
  const { data: extRunLogsData, isLoading: extRunLogsLoading } = useQuery({
    queryKey: ['ext-run-logs', extSelectedRunId],
    queryFn: () =>
      apiGet<{ lines: string[]; next_pos: number; has_more: boolean }>(
        `/api/extensions/runs/${encodeURIComponent(extSelectedRunId!)}/logs`,
        { since_pos: 0, wait: 'true' },
      ),
    enabled: !!extSelectedRunId,
    refetchInterval: (query) => {
      const data = query.state.data
      if (data?.has_more) return 1000
      // 选中 run 还在运行中时持续轮询
      if (extSelectedRun && (extSelectedRun.state === 'running' || extSelectedRun.state === 'pending')) {
        return 1500
      }
      return false
    },
  })

  const handleExport = async () => {
    if (!service) return
    // REQ-2.12.1: 调用后端 export 端点下载 .tar.gz
    try {
      const url = `/api/services/${encodeURIComponent(serviceName)}/export`
      const response = await fetch(url)
      if (!response.ok) throw new Error(`导出失败: ${response.status}`)
      const blob = await response.blob()
      const downloadUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = downloadUrl
      a.download = `${serviceName}.tar.gz`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(downloadUrl)
      toast.success('服务已导出')
    } catch (err) {
      toast.error(getErrorMessage(err, '导出失败'))
    }
  }

  // 计算环境变量三层：服务env / 继承全局env / 合并后预览
  // useMemo 确保引用稳定，避免 EnvSection 的 useEffect 在每次渲染都重置 editEntries
  // 必须在条件 return 之前调用（React Hooks 规则）
  const { serviceEnvEntries, inheritedEnvEntries, mergedEnvEntries } = useMemo(() => {
    const serviceEnvParsed = parseEnvYaml(serviceEnvContent ?? '')
    const serviceKeys = new Set(serviceEnvParsed.map((e) => e.key))
    const inherited = Object.entries(globalEnv?.env ?? {}).map(([k, v]) => ({
      key: k,
      value: v.value ?? '',
      source: 'inherited' as const,
      overridden: serviceKeys.has(k),
    }))
    // 合并：服务env优先覆盖继承env
    const mergedMap = new Map<string, { value: string; source: 'service' | 'inherited' }>()
    for (const e of inherited) {
      mergedMap.set(e.key, { value: e.value, source: 'inherited' })
    }
    for (const e of serviceEnvParsed) {
      mergedMap.set(e.key, { value: e.value, source: 'service' })
    }
    const merged = Array.from(mergedMap.entries()).map(([k, v]) => ({
      key: k,
      value: v.value,
      source: v.source,
    }))
    const service = serviceEnvParsed.map((e) => ({ ...e, source: 'service' as const }))
    return { serviceEnvEntries: service, inheritedEnvEntries: inherited, mergedEnvEntries: merged }
  }, [serviceEnvContent, globalEnv])

  // E-01-001: 加载时显示 skeleton 占位，避免白屏
  if (isLoading) {
    return (
      <div className="space-y-4">
        <SkeletonCard />
        <SkeletonCard />
      </div>
    )
  }

  // E-01-002 修复：服务不存在或加载失败时显示明确提示，不再永远显示 skeleton
  if (isError || !service) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-20">
        <AlertTriangle className="h-10 w-10 text-[var(--color-text-warning)]" />
        <p className="text-sm text-[var(--color-text-secondary)]">
          服务「{serviceName}」不存在或已删除
        </p>
        <Button variant="default" size="sm" onClick={() => navigate('/services')}>
          <ArrowLeft className="h-3.5 w-3.5" />
          返回服务列表
        </Button>
      </div>
    )
  }

  const historyEntries = historyData?.entries ?? []
  const deathEntries = deathsData?.entries ?? []
  const extensions = Array.isArray(serviceExtensions) ? serviceExtensions : []

  return (
    <div className="space-y-4">
      {/* 顶部：返回 + 名称 + 状态 + 操作 */}
      <div className="flex flex-wrap items-center gap-3">
        <Link to="/services" className="flex items-center gap-1 text-sm text-[var(--color-brand-primary)] hover:underline">
          <ArrowLeft className="h-4 w-4" />
          {t.common.back}
        </Link>
        <div className="flex items-center gap-2">
          {service.config?.icon && <IconRenderer name={service.config.icon} className="h-5 w-5 text-[var(--color-brand-primary)]" />}
          <h2 className="text-xl font-semibold text-[var(--color-text-primary)]">{service.name}</h2>
          <Badge variant={stateVariantMap[service.status]}>{t.status[service.status]}</Badge>
        </div>
        <div className="flex-1" />
        {/* 11种边界操作按钮 */}
        <div className="flex flex-wrap gap-2">
          <Button variant="primary" size="sm" onClick={() => startMutation.mutate()} disabled={startMutation.isPending}>
            <Play className="h-3.5 w-3.5" /> {t.service.start}
          </Button>
          <Button variant="danger" size="sm" onClick={() => stopMutation.mutate()} disabled={stopMutation.isPending}>
            <Square className="h-3.5 w-3.5" /> {t.service.stop}
          </Button>
          <Button variant="default" size="sm" onClick={() => restartMutation.mutate()} disabled={restartMutation.isPending}>
            <RotateCw className="h-3.5 w-3.5" /> {t.service.restart}
          </Button>
          <Button variant="default" size="sm" onClick={() => setShowSignalDialog(true)}>
            <Zap className="h-3.5 w-3.5" /> {t.service.sendSignal}
          </Button>
          <Button variant="danger" size="sm" onClick={() => setShowForceStopDialog(true)} disabled={forceStopMutation.isPending}>
            <XCircle className="h-3.5 w-3.5" /> {t.service.forceStop}
          </Button>
          {service.status === 'failed' && (
            <Button variant="default" size="sm" onClick={() => clearFailedMutation.mutate()} disabled={clearFailedMutation.isPending}>
              <AlertTriangle className="h-3.5 w-3.5" /> {t.service.clearFailed}
            </Button>
          )}
          <Button variant="default" size="sm" onClick={() => setActiveTab('config')}>
            <FileText className="h-3.5 w-3.5" /> {t.service.editConfigAction}
          </Button>
          <Button variant="default" size="sm" onClick={() => setActiveTab('env')}>
            <Settings className="h-3.5 w-3.5" /> {t.service.editEnv}
          </Button>
          <Button variant="default" size="sm" onClick={handleExport}>
            <Download className="h-3.5 w-3.5" /> {t.service.export}
          </Button>
          <Button variant="danger" size="sm" onClick={() => setShowDeleteDialog(true)}>
            <Trash2 className="h-3.5 w-3.5" /> {t.service.delete}
          </Button>
          <Button variant="default" size="sm" onClick={() => setActiveTab('logs')}>
            <Terminal className="h-3.5 w-3.5" /> {t.service.viewLogs}
          </Button>
        </div>
      </div>

      {/* 7个标签页 */}
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview"><Server className="h-3.5 w-3.5 mr-1" />{t.service.tabOverview}</TabsTrigger>
          <TabsTrigger value="logs"><Terminal className="h-3.5 w-3.5 mr-1" />进程日志</TabsTrigger>
          <TabsTrigger value="process"><Zap className="h-3.5 w-3.5 mr-1" />{t.service.tabProcess}</TabsTrigger>
          <TabsTrigger value="config"><FileText className="h-3.5 w-3.5 mr-1" />{t.service.tabConfig}</TabsTrigger>
          <TabsTrigger value="env"><Settings className="h-3.5 w-3.5 mr-1" />{t.service.tabEnv}</TabsTrigger>
          <TabsTrigger value="extensions"><Puzzle className="h-3.5 w-3.5 mr-1" />{t.service.tabExtensions}</TabsTrigger>
          <TabsTrigger value="history"><Clock className="h-3.5 w-3.5 mr-1" />{t.service.tabHistory}</TabsTrigger>
        </TabsList>

        {/* 概览 */}
        <TabsContent value="overview">
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader><CardTitle>基本信息</CardTitle></CardHeader>
              <CardContent>
                <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
                  <dt className="text-[var(--color-text-tertiary)]">{t.service.status}</dt>
                  <dd><Badge variant={stateVariantMap[service.status]}>{t.status[service.status]}</Badge></dd>
                  <dt className="text-[var(--color-text-tertiary)]">{t.service.pid}</dt>
                  <dd className="font-mono text-[var(--color-text-primary)]">{service.pid || '-'}</dd>
                  <dt className="text-[var(--color-text-tertiary)]">{t.service.uptime}</dt>
                  <dd className="text-[var(--color-text-primary)]">{formatUptime(service.uptime)}</dd>
                  <dt className="text-[var(--color-text-tertiary)]">{t.service.restarts}</dt>
                  <dd className="text-[var(--color-text-primary)]">{service.restart_count}</dd>
                  {service.config?.user && (
                    <>
                      <dt className="text-[var(--color-text-tertiary)]">{t.service.user}</dt>
                      <dd className="font-mono text-[var(--color-text-primary)]">{service.config.user}</dd>
                    </>
                  )}
                  {service.config?.group && (
                    <>
                      <dt className="text-[var(--color-text-tertiary)]">{t.service.group}</dt>
                      <dd className="font-mono text-[var(--color-text-primary)]">{service.config.group}</dd>
                    </>
                  )}
                </dl>
              </CardContent>
            </Card>
            {/* REQ-I-006: 资源占用 — CPU/内存/磁盘分区/进程数/FD */}
            <Card>
              <CardHeader><CardTitle>资源占用</CardTitle></CardHeader>
              <CardContent>
                {!service.pid ? (
                  <p className="py-4 text-center text-sm text-[var(--color-text-tertiary)]">服务未运行</p>
                ) : !resources ? (
                  <p className="py-4 text-center text-sm text-[var(--color-text-tertiary)]">加载中...</p>
                ) : (
                  <div className="space-y-4">
                    <div className="grid grid-cols-2 gap-4">
                      {/* CPU */}
                      <div className="space-y-1">
                        <div className="flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
                          <Cpu className="h-3.5 w-3.5 text-[var(--color-brand-primary)]" />
                          <span>CPU</span>
                        </div>
                        <div className="text-lg font-semibold text-[var(--color-text-primary)]">
                          {resources.cpu_percent.toFixed(1)}%
                        </div>
                        <div className="h-1.5 w-full rounded-full bg-[var(--color-surface-tertiary)]">
                          <div
                            className="h-1.5 rounded-full bg-[var(--color-brand-primary)] transition-all duration-300"
                            style={{ width: `${Math.min(resources.cpu_percent, 100)}%` }}
                          />
                        </div>
                      </div>
                      {/* 内存 */}
                      <div className="space-y-1">
                        <div className="flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
                          <MemoryStick className="h-3.5 w-3.5 text-[var(--color-text-success)]" />
                          <span>内存 (RSS)</span>
                        </div>
                        <div className="text-lg font-semibold text-[var(--color-text-primary)]">
                          {formatMB(resources.memory_mb)}
                        </div>
                        <div className="text-xs text-[var(--color-text-tertiary)]">
                          {resources.memory_percent.toFixed(1)}% 系统内存
                        </div>
                      </div>
                    </div>
                    {/* 磁盘分区占用 */}
                    {resources.disk_total_mb != null && resources.disk_used_mb != null && (
                      <div className="space-y-1">
                        <div className="flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
                          <HardDrive className="h-3.5 w-3.5 text-[var(--color-accent-warning)]" />
                          <span>工作目录所在分区</span>
                        </div>
                        <div className="flex items-baseline justify-between">
                          <span className="text-lg font-semibold text-[var(--color-text-primary)]">
                            {((resources.disk_used_mb / resources.disk_total_mb) * 100).toFixed(1)}%
                          </span>
                          <span className="text-xs text-[var(--color-text-tertiary)]">
                            {formatMB(resources.disk_used_mb)} / {formatMB(resources.disk_total_mb)}
                          </span>
                        </div>
                        <div className="h-1.5 w-full rounded-full bg-[var(--color-surface-tertiary)]">
                          <div
                            className="h-1.5 rounded-full bg-[var(--color-accent-warning)] transition-all duration-300"
                            style={{ width: `${Math.min((resources.disk_used_mb / resources.disk_total_mb) * 100, 100)}%` }}
                          />
                        </div>
                      </div>
                    )}
                    {/* 进程数 / FD 数 */}
                    <div className="flex gap-6 border-t border-[var(--color-border-secondary)] pt-3 text-xs">
                      <div>
                        <span className="text-[var(--color-text-tertiary)]">进程数：</span>
                        <span className="font-medium text-[var(--color-text-primary)]">{resources.process_count}</span>
                      </div>
                      <div>
                        <span className="text-[var(--color-text-tertiary)]">文件描述符：</span>
                        <span className="font-medium text-[var(--color-text-primary)]">{resources.fd_count}</span>
                      </div>
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
            {/* 端口监听 — 展示服务进程占用的端口，按协议区分，HTTP端口可点击访问 */}
            <Card>
              <CardHeader><CardTitle>监听端口</CardTitle></CardHeader>
              <CardContent>
                {!service.pid ? (
                  <p className="py-4 text-center text-sm text-[var(--color-text-tertiary)]">服务未运行</p>
                ) : !resources ? (
                  <p className="py-4 text-center text-sm text-[var(--color-text-tertiary)]">加载中...</p>
                ) : !resources.ports || resources.ports.length === 0 ? (
                  <p className="py-4 text-center text-sm text-[var(--color-text-tertiary)]">无监听端口</p>
                ) : (
                  <PortList ports={resources.ports} />
                )}
              </CardContent>
            </Card>
            {service.config && (
              <Card>
                <CardHeader><CardTitle>服务配置</CardTitle></CardHeader>
                <CardContent>
                  <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
                    <dt className="text-[var(--color-text-tertiary)]">版本</dt>
                    <dd className="text-[var(--color-text-primary)]">{service.config.version || '-'}</dd>
                    <dt className="text-[var(--color-text-tertiary)]">自启</dt>
                    <dd className="text-[var(--color-text-primary)]">{service.config.autostart ? '是' : '否'}</dd>
                    <dt className="text-[var(--color-text-tertiary)]">命令</dt>
                    <dd className="font-mono text-xs text-[var(--color-text-primary)] truncate max-w-xs" title={service.config.command?.join(' ') ?? ''}>{service.config.command?.join(' ')}</dd>
                    {service.config.runtime && (
                      <>
                        <dt className="text-[var(--color-text-tertiary)]">运行时</dt>
                        <dd className="text-[var(--color-text-primary)]">{service.config.runtime}</dd>
                      </>
                    )}
                    {service.config.workdir && (
                      <>
                        <dt className="text-[var(--color-text-tertiary)]">工作目录</dt>
                        <dd className="font-mono text-xs text-[var(--color-text-primary)]">{service.config.workdir}</dd>
                      </>
                    )}
                  </dl>
                  {service.config.depends_on && service.config.depends_on.length > 0 && (
                    <div className="mt-3">
                      <span className="text-sm text-[var(--color-text-tertiary)]">{t.service.dependencies}：</span>
                      <div className="mt-1 flex flex-wrap gap-1">
                        {service.config.depends_on.map((dep) => (
                          <Link key={dep} to={`/services/${dep}`}>
                            <Badge variant="info">{dep}</Badge>
                          </Link>
                        ))}
                      </div>
                    </div>
                  )}
                  {service.config.tags && service.config.tags.length > 0 && (
                    <div className="mt-3">
                      <span className="text-sm text-[var(--color-text-tertiary)]">{t.service.tags}：</span>
                      <div className="mt-1 flex flex-wrap gap-1">
                        {service.config.tags.map((tag) => (
                          <Badge key={tag} variant="secondary">{tag}</Badge>
                        ))}
                      </div>
                    </div>
                  )}
                </CardContent>
              </Card>
            )}
            {service.pending_changes && service.pending_changes.length > 0 && (
              <Card>
                <CardHeader><CardTitle>待生效变更</CardTitle></CardHeader>
                <CardContent>
                  <ul className="space-y-1">
                    {service.pending_changes.map((change, idx) => (
                      <li key={idx} className="text-sm text-[var(--color-text-warning)]">{change}</li>
                    ))}
                  </ul>
                </CardContent>
              </Card>
            )}
          </div>
        </TabsContent>

        {/* 进程日志 — 仅服务进程日志，扩展日志入口在"扩展"标签内 */}
        <TabsContent value="logs">
          <div className="space-y-2">
            <div className="text-xs text-[var(--color-text-tertiary)]">
              仅显示服务进程的 stdout/stderr 日志。扩展运行日志请至"扩展"标签查看。
            </div>
            <LogViewer serviceName={serviceName} />
          </div>
        </TabsContent>

        {/* 进程 */}
        <TabsContent value="process">
          <ProcessTree serviceName={serviceName} />
        </TabsContent>

        {/* 配置 — REQ-2.10/2.11.3: 可视化编辑 / YAML编辑切换 */}
        <TabsContent value="config">
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>{t.service.editConfig}</CardTitle>
                <div className="flex items-center gap-2">
                  <div className="flex items-center gap-1 rounded-lg bg-[var(--color-bg-tertiary)] p-1">
                    <button
                      onClick={() => setConfigEditMode('visual')}
                      className={`rounded-md px-3 py-1 text-xs font-medium transition-colors ${configEditMode === 'visual' ? 'bg-[var(--color-surface-primary)] shadow-sm text-[var(--color-text-primary)]' : 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}`}
                    >
                      可视化编辑
                    </button>
                    <button
                      onClick={() => setConfigEditMode('yaml')}
                      className={`rounded-md px-3 py-1 text-xs font-medium transition-colors ${configEditMode === 'yaml' ? 'bg-[var(--color-surface-primary)] shadow-sm text-[var(--color-text-primary)]' : 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}`}
                    >
                      YAML编辑
                    </button>
                  </div>
                  {configEditMode === 'yaml' && (
                    <Button
                      variant="default"
                      size="sm"
                      onClick={() => saveConfigMutation.mutate(configContent)}
                      disabled={saveConfigMutation.isPending}
                    >
                      {t.service.save}
                    </Button>
                  )}
                </div>
              </div>
            </CardHeader>
            <CardContent>
              {configEditMode === 'visual' ? (
                <div className="max-h-[600px] overflow-y-auto pr-1">
                  <ServiceForm
                    key="visual-edit"
                    initial={parsedConfig}
                    onSubmit={handleVisualSave}
                    onCancel={() => setConfigEditMode('yaml')}
                    submitLabel={t.service.save}
                    isLoading={saveConfigMutation.isPending}
                  />
                </div>
              ) : (
                <div className="h-[500px] w-full overflow-hidden rounded-md border border-[var(--color-border-secondary)]">
                  <MonacoEditor
                    value={configContent}
                    onChange={setConfigContent}
                    onSave={(val) => saveConfigMutation.mutate(val)}
                    filename={`${serviceName}-service.yaml`}
                    height="500px"
                  />
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        {/* 环境变量 */}
        <TabsContent value="env">
          <EnvEditor
            serviceName={serviceName}
            serviceEnv={serviceEnvEntries}
            inheritedEnv={inheritedEnvEntries}
            mergedEnv={mergedEnvEntries}
          />
        </TabsContent>

        {/* 扩展 — 服务级扩展列表（增强版：详情+操作+触发器+日志入口+CRUD） */}
        <TabsContent value="extensions">
          {/* E-09-001: 顶部工具栏 — 添加扩展按钮 */}
          <div className="flex items-center justify-between mb-3">
            <div className="text-xs text-[var(--color-text-tertiary)]">
              {extensions.length > 0
                ? `共 ${extensions.length} 个服务级扩展。触发器列出该扩展所有触发条件；动作按钮可直接运行对应 action。`
                : '暂无服务级扩展，点击右侧"添加扩展"创建。'}
            </div>
            <Button variant="primary" size="sm" onClick={handleCreateExtension}>
              <Plus className="h-3.5 w-3.5" />
              添加扩展
            </Button>
          </div>
          {extensions.length === 0 ? (
            <Card>
              <CardContent className="py-8 text-center text-sm text-[var(--color-text-tertiary)]">
                {t.common.noData}
              </CardContent>
            </Card>
          ) : (
            <div className="space-y-3">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>名称</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>触发条件</TableHead>
                    <TableHead>动作按钮</TableHead>
                    <TableHead className="text-right">运行/成功/失败</TableHead>
                    <TableHead>最近运行</TableHead>
                    <TableHead className="text-right">日志/历史</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {extensions.map((ext) => {
                    // 收集该扩展所有触发条件
                    const triggerBadges: Array<{ key: string; label: string; variant: 'default' | 'info' | 'warning' | 'success' | 'secondary' }> = []
                    if (ext.triggers?.on_demand) {
                      triggerBadges.push({ key: 'on_demand', label: '手动', variant: 'info' })
                    }
                    if (ext.triggers?.on_schedule && ext.triggers.on_schedule.length > 0) {
                      ext.triggers.on_schedule.forEach((s, i) => {
                        triggerBadges.push({ key: `sched-${i}`, label: `定时:${s.cron}`, variant: 'default' })
                      })
                    }
                    if (ext.triggers?.service_lifecycle && ext.triggers.service_lifecycle.length > 0) {
                      ext.triggers.service_lifecycle.forEach((s, i) => {
                        triggerBadges.push({ key: `svc-${i}`, label: `服务:${s.event}`, variant: 'warning' })
                      })
                    }
                    if (ext.triggers?.supd_lifecycle && ext.triggers.supd_lifecycle.length > 0) {
                      ext.triggers.supd_lifecycle.forEach((s, i) => {
                        triggerBadges.push({ key: `supd-${i}`, label: `supd:${s.event}`, variant: 'secondary' })
                      })
                    }
                    // 无任何触发器时回退到 trigger_type 标签
                    if (triggerBadges.length === 0 && ext.trigger_type) {
                      const triggerLabelMap: Record<string, string> = {
                        on_demand: '手动触发',
                        on_schedule: '定时触发',
                        service_lifecycle: '服务生命周期',
                        supd_lifecycle: 'supd生命周期',
                      }
                      triggerBadges.push({ key: ext.trigger_type, label: triggerLabelMap[ext.trigger_type] ?? ext.trigger_type, variant: 'default' })
                    }

                    // 动作按钮列表
                    const actionList = ext.actions ?? []
                    const actionButtonVariant = (style?: string): 'primary' | 'default' | 'danger' => {
                      if (style === 'primary') return 'primary'
                      if (style === 'danger') return 'danger'
                      return 'default'
                    }

                    return (
                      <TableRow key={ext.name}>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <Puzzle className="h-4 w-4 text-[var(--color-text-tertiary)] shrink-0" />
                            <div>
                              <Link
                                to={`/extensions/${encodeURIComponent(ext.name)}`}
                                className="font-medium text-[var(--color-brand-primary)] hover:underline"
                              >
                                {ext.name}
                              </Link>
                              {ext.version && (
                                <div className="text-xs text-[var(--color-text-tertiary)]">v{ext.version}</div>
                              )}
                              {ext.description && (
                                <div className="text-xs text-[var(--color-text-tertiary)] truncate max-w-xs" title={ext.description}>
                                  {ext.description}
                                </div>
                              )}
                              {ext.concurrency && (
                                <div className="text-[10px] text-[var(--color-text-tertiary)] mt-0.5">并发: {ext.concurrency}</div>
                              )}
                            </div>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant={ext.enabled ? 'success' : 'secondary'}>
                            {ext.enabled ? '启用' : '禁用'}
                          </Badge>
                          {ext.display_state && (
                            <div className="text-[10px] text-[var(--color-text-tertiary)] mt-1">{ext.display_state}</div>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1 max-w-xs">
                            {triggerBadges.length > 0 ? (
                              triggerBadges.map((tb) => (
                                <Badge key={tb.key} variant={tb.variant} className="text-[10px]">
                                  {tb.label}
                                </Badge>
                              ))
                            ) : (
                              <span className="text-xs text-[var(--color-text-tertiary)]">-</span>
                            )}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1 max-w-xs">
                            {actionList.length > 0 ? (
                              actionList.map((act) => (
                                <Button
                                  key={act.id}
                                  variant={actionButtonVariant(act.button_style)}
                                  size="sm"
                                  onClick={() => runExtensionWithLog(ext.name, act.id)}
                                  disabled={!ext.enabled}
                                  title={ext.enabled ? `运行 action: ${act.id}` : '扩展已禁用'}
                                >
                                  <Play className="h-3 w-3" />
                                  {act.label || act.id}
                                </Button>
                              ))
                            ) : (
                              <Button
                                variant="primary"
                                size="sm"
                                onClick={() => runExtensionWithLog(ext.name)}
                                disabled={!ext.enabled}
                                title={ext.enabled ? '运行扩展' : '扩展已禁用'}
                              >
                                <Play className="h-3 w-3" />
                                运行
                              </Button>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="text-right text-xs font-mono">
                          <span className="text-[var(--color-text-primary)]">{ext.run_count ?? 0}</span>
                          {' / '}
                          <span className="text-[var(--color-text-success)]">{ext.success_count ?? 0}</span>
                          {' / '}
                          <span className="text-[var(--color-text-error)]">{ext.fail_count ?? 0}</span>
                        </TableCell>
                        <TableCell className="text-xs text-[var(--color-text-tertiary)]">
                          {ext.last_run_at || '-'}
                          {ext.last_status && (
                            <Badge
                              variant={
                                ext.last_status === 'success' ? 'success'
                                  : ext.last_status === 'failed' ? 'danger'
                                  : 'secondary'
                              }
                              className="ml-1 text-[10px]"
                            >
                              {ext.last_status}
                            </Badge>
                          )}
                        </TableCell>
                        <TableCell className="text-right">
                          <div className="flex items-center justify-end gap-1">
                            {/* E-09-001: 编辑扩展 meta 配置 */}
                            <Button
                              variant="default"
                              size="sm"
                              onClick={() => handleEditExtension(ext.name)}
                              title="编辑扩展配置"
                            >
                              <Pencil className="h-3.5 w-3.5" />
                            </Button>
                            {/* E-09-001: 删除扩展（带确认） */}
                            <Button
                              variant="danger"
                              size="sm"
                              onClick={() => setShowExtDeleteDialog(ext.name)}
                              title="删除扩展"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                            <Button
                              variant="default"
                              size="sm"
                              onClick={() => { setExtLogExt(ext.name); setExtLogSelectedRunId(null) }}
                              title="查看运行历史与日志"
                            >
                              <FileText className="h-3.5 w-3.5" />
                              日志
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </TabsContent>

        {/* 历史 — 启动历史 + 死亡历史 */}
        <TabsContent value="history">
          <div className="space-y-4">
            {/* 启动历史 */}
            <Card>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle>{t.service.startupHistory}</CardTitle>
                  <span className="text-sm text-[var(--color-text-tertiary)]">{t.service.recent20}</span>
                </div>
              </CardHeader>
              <CardContent>
                {historyEntries.length === 0 ? (
                  <p className="text-center text-[var(--color-text-tertiary)] py-8">{t.common.noData}</p>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>时间</TableHead>
                        <TableHead>PID</TableHead>
                        <TableHead>运行时长</TableHead>
                        <TableHead>退出码</TableHead>
                        <TableHead>原因</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {historyEntries.map((entry, idx) => (
                        <TableRow key={`${entry.time}-${idx}`}>
                          <TableCell className="text-xs">{entry.time}</TableCell>
                          <TableCell className="font-mono text-xs">{entry.pid || '-'}</TableCell>
                          <TableCell className="text-xs">
                            {entry.duration_seconds > 0 ? `${entry.duration_seconds}s` : '-'}
                          </TableCell>
                          <TableCell className="font-mono text-xs">{entry.exit_code}</TableCell>
                          <TableCell className="text-xs text-[var(--color-text-secondary)]">{entry.reason || '-'}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </CardContent>
            </Card>

            {/* 死亡历史 */}
            <Card>
              <CardHeader>
                <CardTitle>{t.service.deathHistory}</CardTitle>
              </CardHeader>
              <CardContent>
                {deathEntries.length === 0 ? (
                  <p className="text-center text-[var(--color-text-tertiary)] py-8">{t.common.noData}</p>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>时间</TableHead>
                        <TableHead>PID</TableHead>
                        <TableHead>退出码</TableHead>
                        <TableHead>原因</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {deathEntries.map((entry, idx) => (
                        <TableRow key={`death-${entry.time}-${idx}`}>
                          <TableCell className="text-xs">{entry.time}</TableCell>
                          <TableCell className="font-mono text-xs">{entry.pid || '-'}</TableCell>
                          <TableCell className="font-mono text-xs">
                            <Badge variant={entry.exit_code === 0 ? 'success' : 'danger'}>
                              {entry.exit_code}
                            </Badge>
                          </TableCell>
                          <TableCell className="text-xs text-[var(--color-text-secondary)]">{entry.reason || '-'}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </CardContent>
            </Card>
          </div>
        </TabsContent>
      </Tabs>

      {/* 发送信号对话框 */}
      {showSignalDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" onClick={() => setShowSignalDialog(false)} />
          <div className="relative z-10 w-full max-w-lg rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-6 shadow-[var(--shadow-lg)]">
            <DialogHeader>
              <DialogTitle>{t.service.sendSignal}</DialogTitle>
            </DialogHeader>
            <div className="py-4">
              <Input
                value={signalInput}
                onChange={(e) => setSignalInput(e.target.value)}
                placeholder="信号名称 (如 SIGTERM)"
              />
            </div>
            <DialogFooter>
              <Button variant="default" onClick={() => setShowSignalDialog(false)}>{t.common.cancel}</Button>
              <Button variant="primary" onClick={() => signalMutation.mutate(signalInput)} disabled={signalMutation.isPending}>
                {t.service.sendSignal}
              </Button>
            </DialogFooter>
          </div>
        </div>
      )}

      {/* 删除确认对话框 — REQ-2.9.12: 危险操作点击遮罩不关闭 */}
      {showDeleteDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" />
          <div className="relative z-10 w-full max-w-lg rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-6 shadow-[var(--shadow-lg)]">
            <DialogHeader>
              <DialogTitle>{t.service.delete}</DialogTitle>
            </DialogHeader>
            <p className="text-sm text-[var(--color-text-secondary)]">
              确定要删除服务 <span className="font-semibold text-[var(--color-text-primary)]">{serviceName}</span> 吗？此操作不可恢复。
            </p>
            <DialogFooter>
              <Button variant="default" onClick={() => setShowDeleteDialog(false)}>{t.common.cancel}</Button>
              <Button variant="danger" onClick={() => deleteMutation.mutate()} disabled={deleteMutation.isPending}>
                {t.service.delete}
              </Button>
            </DialogFooter>
          </div>
        </div>
      )}

      {/* 强制停止确认弹窗 — 危险操作，点击遮罩不关闭 */}
      {showForceStopDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" />
          <div className="relative z-10 w-full max-w-sm rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-6 shadow-md">
            <DialogHeader>
              <DialogTitle>{t.service.forceStop}</DialogTitle>
            </DialogHeader>
            <p className="text-sm text-[var(--color-text-secondary)]">
              确定要强制停止服务 <span className="font-semibold text-[var(--color-text-primary)]">{serviceName}</span> 吗？将直接发送 SIGKILL 信号，可能导致数据损坏。
            </p>
            <DialogFooter>
              <Button variant="default" onClick={() => setShowForceStopDialog(false)}>{t.common.cancel}</Button>
              <Button
                variant="danger"
                onClick={() => {
                  forceStopMutation.mutate()
                  setShowForceStopDialog(false)
                }}
                disabled={forceStopMutation.isPending}
              >
                {t.service.forceStop}
              </Button>
            </DialogFooter>
          </div>
        </div>
      )}

      {/* E-09-001: 扩展创建/编辑对话框 — 可视化表单，头部含保存按钮 */}
      {showExtFormDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" onClick={() => setShowExtFormDialog(false)} />
          <div className="relative z-10 w-full max-w-2xl max-h-[80vh] flex flex-col rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] shadow-md">
            {/* 头部 — 含标题与操作按钮（保存/关闭） */}
            <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-5 py-3 shrink-0">
              <div className="flex items-center gap-2">
                <Puzzle className="h-4 w-4 text-[var(--color-text-tertiary)]" />
                <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">
                  {editingExtName ? `编辑扩展: ${editingExtName}` : '添加扩展'}
                </h3>
              </div>
              <div className="flex items-center gap-1">
                <Button
                  variant="primary"
                  size="sm"
                  onClick={handleSaveExtension}
                  disabled={createExtMutation.isPending || updateExtMutation.isPending}
                >
                  {createExtMutation.isPending || updateExtMutation.isPending ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Download className="h-3.5 w-3.5" />
                  )}
                  保存
                </Button>
                <button
                  onClick={() => setShowExtFormDialog(false)}
                  className="rounded-sm p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
            </div>
            {/* 可视化表单区域 */}
            <div className="flex-1 overflow-y-auto p-4 space-y-4">
              {/* 基本信息 */}
              <div className="space-y-3">
                <div className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase tracking-wide">基本信息</div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="text-xs text-[var(--color-text-tertiary)]">扩展名称 <span className="text-[var(--color-brand-primary)]">*</span></label>
                    <Input
                      value={extForm?.name ?? ''}
                      onChange={(e) => setExtForm(f => f ? { ...f, name: e.target.value } : f)}
                      className="mt-1 h-8 text-sm font-mono"
                      placeholder="my-extension"
                      disabled={!!editingExtName}
                    />
                  </div>
                  <div>
                    <label className="text-xs text-[var(--color-text-tertiary)]">版本</label>
                    <Input
                      value={extForm?.version ?? ''}
                      onChange={(e) => setExtForm(f => f ? { ...f, version: e.target.value } : f)}
                      className="mt-1 h-8 text-sm font-mono"
                      placeholder="1.0.0"
                    />
                  </div>
                </div>
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">描述</label>
                  <Input
                    value={extForm?.description ?? ''}
                    onChange={(e) => setExtForm(f => f ? { ...f, description: e.target.value } : f)}
                    className="mt-1 h-8 text-sm"
                    placeholder="扩展描述（可选）"
                  />
                </div>
              </div>

              {/* 执行配置 */}
              <div className="space-y-3">
                <div className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase tracking-wide">执行配置</div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="text-xs text-[var(--color-text-tertiary)]">入口脚本</label>
                    <Input
                      value={extForm?.entry ?? ''}
                      onChange={(e) => setExtForm(f => f ? { ...f, entry: e.target.value } : f)}
                      className="mt-1 h-8 text-sm font-mono"
                      placeholder="run.sh"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-[var(--color-text-tertiary)]">运行时</label>
                    <Input
                      value={extForm?.runtime ?? ''}
                      onChange={(e) => setExtForm(f => f ? { ...f, runtime: e.target.value } : f)}
                      className="mt-1 h-8 text-sm font-mono"
                      placeholder="bash / sh / python3 / node 或绝对路径"
                      list="ext-form-runtime-options"
                    />
                    <datalist id="ext-form-runtime-options">
                      {runtimeOptions.filter((o) => o.value).map((o) => (
                        <option key={o.value} value={o.value} />
                      ))}
                    </datalist>
                  </div>
                  <div>
                    <label className="text-xs text-[var(--color-text-tertiary)]">超时(秒,1-1800)</label>
                    <Input
                      type="number"
                      min={1}
                      max={1800}
                      value={extForm?.timeout_seconds ?? 600}
                      onChange={(e) => {
                        let v = Number(e.target.value) || 600
                        if (v > 1800) v = 1800
                        if (v < 1) v = 1
                        setExtForm(f => f ? { ...f, timeout_seconds: v } : f)
                      }}
                      className="mt-1 h-8 text-sm"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-[var(--color-text-tertiary)]">并发策略</label>
                    <Select
                      className="mt-1 w-full"
                      value={extForm?.concurrency ?? 'replace'}
                      onChange={(e) => setExtForm(f => f ? { ...f, concurrency: e.target.value } : f)}
                      options={[
                        { value: 'replace', label: 'replace — 替换(新任务终止旧任务)' },
                        { value: 'serialize', label: 'serialize — 串行排队' },
                        { value: 'parallel', label: 'parallel — 并行执行' },
                        { value: 'debounce:Ns', label: 'debounce:Ns — 防抖' },
                      ]}
                    />
                  </div>
                </div>

                {/* §2.2.13: 执行身份 — User 模式与 UID 模式互斥，留空则继承服务身份 */}
                <div className="mt-3 border-t border-[var(--color-border-secondary)] pt-3">
                  <div className="flex items-center justify-between">
                    <label className="text-xs text-[var(--color-text-tertiary)]">执行身份</label>
                    <div className="flex gap-1">
                      <button
                        type="button"
                        onClick={() => setExtForm(f => f ? { ...f, identityMode: 'user' } : f)}
                        className={`px-2.5 py-1 text-xs rounded border transition-colors ${extForm?.identityMode === 'user' ? 'bg-[var(--color-brand-primary)] text-white border-[var(--color-brand-primary)]' : 'border-[var(--color-border-secondary)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-secondary)]'}`}
                      >
                        按用户名（run_as）
                      </button>
                      <button
                        type="button"
                        onClick={() => setExtForm(f => f ? { ...f, identityMode: 'uid' } : f)}
                        className={`px-2.5 py-1 text-xs rounded border transition-colors ${extForm?.identityMode === 'uid' ? 'bg-[var(--color-brand-primary)] text-white border-[var(--color-brand-primary)]' : 'border-[var(--color-border-secondary)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-secondary)]'}`}
                      >
                        按 UID（run_as_uid）
                      </button>
                    </div>
                  </div>
                  <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">
                    {extForm?.identityMode === 'uid'
                      ? '直接指定数字 uid/gid，不依赖 /etc/passwd（适用于 NAS 固定 uid 服务）；留空则继承服务身份'
                      : '通过用户名查找（需存在于 /etc/passwd）；留空则继承服务身份'}
                  </p>
                  {extForm?.identityMode === 'uid' ? (
                    <div className="mt-2 grid grid-cols-3 gap-3">
                      <Input
                        type="number"
                        value={extForm?.run_as_uid ?? ''}
                        onChange={(e) => setExtForm(f => f ? { ...f, run_as_uid: e.target.value } : f)}
                        className="h-8 text-sm font-mono"
                        placeholder="UID"
                      />
                      <Input
                        type="number"
                        value={extForm?.run_as_gid ?? ''}
                        onChange={(e) => setExtForm(f => f ? { ...f, run_as_gid: e.target.value } : f)}
                        className="h-8 text-sm font-mono"
                        placeholder="GID（留空=UID）"
                      />
                      <Input
                        value={extForm?.run_as_groups ?? ''}
                        onChange={(e) => setExtForm(f => f ? { ...f, run_as_groups: e.target.value } : f)}
                        className="h-8 text-sm font-mono"
                        placeholder="补充组（逗号分隔）"
                      />
                    </div>
                  ) : (
                    <div className="mt-2">
                      <Input
                        value={extForm?.run_as ?? ''}
                        onChange={(e) => setExtForm(f => f ? { ...f, run_as: e.target.value } : f)}
                        className="h-8 text-sm font-mono"
                        placeholder="root（留空继承服务用户）"
                      />
                    </div>
                  )}
                </div>
              </div>

              {/* 触发器 */}
              <div className="space-y-3">
                <div className="text-xs font-semibold text-[var(--color-text-tertiary)] uppercase tracking-wide">触发器</div>
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">手动触发 (on_demand)</label>
                  <div className="mt-1">
                    <button
                      type="button"
                      onClick={() => setExtForm(f => f ? { ...f, triggers_on_demand: !f.triggers_on_demand } : f)}
                      className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm border transition-colors ${
                        extForm?.triggers_on_demand
                          ? 'border-[var(--color-brand-primary)] bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)]'
                          : 'border-[var(--color-border-secondary)] text-[var(--color-text-tertiary)]'
                      }`}
                    >
                      {extForm?.triggers_on_demand ? <ToggleRight className="h-4 w-4" /> : <ToggleLeft className="h-4 w-4" />}
                      {extForm?.triggers_on_demand ? '已启用' : '已禁用'}
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* E-09-001: 扩展删除确认对话框 */}
      {showExtDeleteDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" />
          <div className="relative z-10 w-full max-w-sm rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-6 shadow-md">
            <DialogHeader>
              <DialogTitle>删除扩展</DialogTitle>
            </DialogHeader>
            <p className="text-sm text-[var(--color-text-secondary)]">
              确定要删除扩展 <span className="font-semibold text-[var(--color-text-primary)]">{showExtDeleteDialog}</span> 吗？此操作不可撤销，将删除扩展目录及所有文件。
            </p>
            <DialogFooter>
              <Button variant="default" onClick={() => setShowExtDeleteDialog(null)}>{t.common.cancel}</Button>
              <Button
                variant="danger"
                onClick={() => deleteExtMutation.mutate(showExtDeleteDialog)}
                disabled={deleteExtMutation.isPending}
              >
                {deleteExtMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                删除
              </Button>
            </DialogFooter>
          </div>
        </div>
      )}

      {/* 扩展运行日志对话框 — 统一左右分栏：左侧运行列表 + 右侧日志内容 */}
      {extLogExt && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" onClick={() => { setExtLogExt(null); setExtLogSelectedRunId(null) }} />
          <div className="relative z-10 w-full max-w-5xl max-h-[85vh] flex flex-col rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] shadow-md">
            {/* 头部 */}
            <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-5 py-3 shrink-0">
              <div className="flex items-center gap-2">
                <FileText className="h-4 w-4 text-[var(--color-text-tertiary)]" />
                <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">扩展运行日志</h3>
                <Badge variant="secondary" className="font-mono text-xs">{extLogExt}</Badge>
              </div>
              <div className="flex items-center gap-1">
                <button
                  onClick={() => {
                    if (!extSelectedRunId) return
                    apiDelete(`/api/extensions/runs/${encodeURIComponent(extSelectedRunId)}/logs`).then(() => {
                      queryClient.invalidateQueries({ queryKey: ['ext-run-logs', extSelectedRunId] })
                    })
                  }}
                  disabled={!extSelectedRunId}
                  className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)] rounded px-2 py-1 flex items-center gap-1 disabled:opacity-40 disabled:cursor-not-allowed"
                  title="清空当前运行的日志"
                >
                  <Eraser className="h-3.5 w-3.5" /> 清空日志
                </button>
                <button
                  onClick={() => {
                    apiDelete(`/api/extensions/runs?service_name=${encodeURIComponent(serviceName)}&extension_name=${encodeURIComponent(extLogExt!)}`).then(() => {
                      queryClient.invalidateQueries({ queryKey: ['service-ext-runs', serviceName, extLogExt] })
                      setExtLogSelectedRunId(null)
                    })
                  }}
                  disabled={extRuns.length === 0}
                  className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-error)] hover:bg-[var(--color-surface-hover)] rounded px-2 py-1 flex items-center gap-1 disabled:opacity-40 disabled:cursor-not-allowed"
                  title="清空所有运行记录（仅终态）"
                >
                  <Trash2 className="h-3.5 w-3.5" /> 清空记录
                </button>
                <button
                  onClick={() => { setExtLogExt(null); setExtLogSelectedRunId(null) }}
                  className="rounded-sm p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
            </div>
            {/* 左右分栏内容 */}
            <div className="flex flex-1 overflow-hidden">
              {/* 左侧：运行记录列表 */}
              <div className="w-64 shrink-0 border-r border-[var(--color-border-secondary)] overflow-auto bg-[var(--color-bg-tertiary)]">
                {extRunsLoading ? (
                  <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                    <Loader2 className="h-4 w-4 animate-spin mr-2" />加载中...
                  </div>
                ) : extRuns.length === 0 ? (
                  <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                    暂无运行记录
                  </div>
                ) : (
                  <div className="py-1">
                    {extRuns.slice(0, 50).map((run) => {
                      const isActive = run.run_id === extSelectedRunId
                      return (
                        <button
                          key={run.run_id}
                          onClick={() => setExtLogSelectedRunId(run.run_id)}
                          className={`w-full text-left px-3 py-2 border-b border-[var(--color-border-secondary)] transition-colors ${
                            isActive
                              ? 'bg-[var(--color-surface-hover)] border-l-2 border-l-[var(--color-brand-primary)]'
                              : 'hover:bg-[var(--color-surface-hover)]'
                          }`}
                        >
                          <div className="flex items-center justify-between gap-1">
                            <span className="text-xs font-mono text-[var(--color-text-tertiary)]">
                              {run.run_id.slice(0, 8)}
                            </span>
                            {run.state === 'running' || run.state === 'pending' ? (
                              <span className="flex items-center gap-1 text-[10px] text-[var(--color-brand-primary)]">
                                <Loader2 className="h-2.5 w-2.5 animate-spin" />
                                {run.progress ?? 0}%
                              </span>
                            ) : (
                              <span className={`text-[10px] font-medium ${
                                run.state === 'success' ? 'text-[var(--color-text-success)]'
                                  : run.state === 'failed' || run.state === 'killed' ? 'text-[var(--color-text-error)]'
                                  : 'text-[var(--color-text-tertiary)]'
                              }`}>
                                {run.state}
                              </span>
                            )}
                          </div>
                          <div className="text-[10px] text-[var(--color-text-tertiary)] mt-0.5 truncate" title={`${run.action_id ? run.action_id + ' · ' : ''}${run.started_at ? new Date(run.started_at).toLocaleString('zh-CN') : '-'}`}>
                            {run.action_id ? `${run.action_id} · ` : ''}
                            {run.started_at ? new Date(run.started_at).toLocaleString('zh-CN') : '-'}
                          </div>
                          {run.result_msg && (
                            <div className="text-[10px] text-[var(--color-text-secondary)] mt-0.5 truncate" title={run.result_msg}>
                              {run.result_msg}
                            </div>
                          )}
                        </button>
                      )
                    })}
                  </div>
                )}
              </div>
              {/* 右侧：日志内容 */}
              <div className="flex-1 flex flex-col overflow-hidden">
                {/* 选中 run 的状态栏 */}
                {extSelectedRun && (
                  <div className="flex items-center gap-2 px-4 py-2 border-b border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] shrink-0">
                    <Badge
                      variant={
                        extSelectedRun.state === 'success' ? 'success'
                          : extSelectedRun.state === 'failed' || extSelectedRun.state === 'killed' ? 'danger'
                          : extSelectedRun.state === 'running' || extSelectedRun.state === 'pending' ? 'info'
                          : 'secondary'
                      }
                    >
                      {extSelectedRun.state === 'running' || extSelectedRun.state === 'pending' ? (
                        <span className="flex items-center gap-1">
                          <Loader2 className="h-3 w-3 animate-spin" />
                          {extSelectedRun.state} {extSelectedRun.progress ?? 0}%
                        </span>
                      ) : extSelectedRun.state}
                    </Badge>
                    <span className="text-xs text-[var(--color-text-tertiary)] font-mono">
                      {extSelectedRun.action_id || 'default'}
                    </span>
                    <span className="text-xs text-[var(--color-text-tertiary)]">
                      {extSelectedRun.started_at}
                    </span>
                    {extSelectedRun.result_msg && (
                      <span className="text-xs text-[var(--color-text-secondary)] truncate flex-1" title={extSelectedRun.result_msg}>
                        {extSelectedRun.result_msg}
                      </span>
                    )}
                    <Link
                      to={`/extensions/${encodeURIComponent(extLogExt)}`}
                      className="text-xs text-[var(--color-brand-primary)] hover:underline shrink-0"
                      onClick={() => { setExtLogExt(null); setExtLogSelectedRunId(null) }}
                    >
                      扩展详情 →
                    </Link>
                  </div>
                )}
                {/* 日志文本 */}
                <div className="flex-1 overflow-auto p-4">
                  {extRunLogsLoading ? (
                    <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                      <Loader2 className="h-4 w-4 animate-spin mr-2" />加载中...
                    </div>
                  ) : !extSelectedRunId ? (
                    <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                      该扩展暂无运行记录
                    </div>
                  ) : (extRunLogsData?.lines?.length ?? 0) > 0 ? (
                    <pre className="whitespace-pre-wrap break-all text-xs font-mono text-[var(--color-text-secondary)] leading-relaxed">
                      {extRunLogsData!.lines.join('\n')}
                    </pre>
                  ) : (
                    <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                      此次运行无日志输出
                    </div>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// ===== 端口列表组件 =====

/** 构造可访问的 URL（0.0.0.0/:: → 当前页面 host，确保远程访问可用） */
function buildPortUrl(scheme: 'http' | 'https', address: string, port: number): string {
  let host = address
  // 监听所有地址的服务，用当前页面 host（远程访问时为 NAS IP，而非浏览器本机 127.0.0.1）
  if (host === '0.0.0.0' || host === '::' || host === '') host = window.location.hostname
  // IPv6 地址需要加方括号
  if (host.includes(':') && !host.startsWith('[')) host = `[${host}]`
  // 默认端口省略
  if ((scheme === 'http' && port === 80) || (scheme === 'https' && port === 443)) {
    return `${scheme}://${host}`
  }
  return `${scheme}://${host}:${port}`
}

/** 端口列表：按协议族分组展示，HTTP 端口由浏览器探测判定，可点击访问 */
function PortList({ ports }: { ports: PortInfo[] }) {
  const probedPorts = useHTTPProbe(ports)

  // 按协议族分组：TCP (tcp+tcp6) / UDP (udp+udp6)
  const tcpPorts = probedPorts.filter((p) => p.protocol.startsWith('tcp'))
  const udpPorts = probedPorts.filter((p) => p.protocol.startsWith('udp'))
  // 端口去重（同一端口可能同时有 tcp/tcp6 条目）
  const dedup = (list: ProbedPortInfo[]) => {
    const seen = new Set<string>()
    return list.filter((p) => {
      const key = `${p.protocol}-${p.address}-${p.port}`
      if (seen.has(key)) return false
      seen.add(key)
      return true
    })
  }
  const tcp = dedup(tcpPorts).sort((a, b) => {
    // HTTP 端口优先
    if (a.is_http !== b.is_http) return a.is_http ? -1 : 1
    return a.port - b.port
  })
  const udp = dedup(udpPorts).sort((a, b) => a.port - b.port)

  const renderPort = (p: ProbedPortInfo, isTcp: boolean) => {
    // 使用前端探测结果判定 HTTP
    const url = (isTcp && p.is_http) ? buildPortUrl('http', p.address, p.port) : null
    const addrDisplay = p.address === '0.0.0.0' ? '*' : p.address === '::' ? '*' : p.address
    return (
      <div key={`${p.protocol}-${p.address}-${p.port}`} className="flex items-center gap-2 py-1.5 text-sm">
        <span
          className={`inline-flex h-5 w-10 shrink-0 items-center justify-center rounded text-[10px] font-medium ${
            isTcp
              ? 'bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)]'
              : 'bg-[var(--color-accent-warning)]/10 text-[var(--color-accent-warning)]'
          }`}
        >
          {isTcp ? 'TCP' : 'UDP'}
        </span>
        {url ? (
          <a
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 font-mono text-[var(--color-brand-primary)] hover:underline"
            title={`点击访问 ${url}`}
          >
            <span>{addrDisplay}:{p.port}</span>
            <ExternalLink className="h-3 w-3" />
          </a>
        ) : (
          <span className="font-mono text-[var(--color-text-primary)]">
            {addrDisplay}:{p.port}
          </span>
        )}
        {p.protocol.endsWith('6') && (
          <span className="text-[10px] text-[var(--color-text-tertiary)]">IPv6</span>
        )}
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {tcp.length > 0 && (
        <div>
          <div className="mb-1 flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
            <Network className="h-3.5 w-3.5" />
            <span>TCP 监听 ({tcp.length})</span>
          </div>
          <div className="divide-y divide-[var(--color-border-secondary)]">
            {tcp.map((p) => renderPort(p, true))}
          </div>
        </div>
      )}
      {udp.length > 0 && (
        <div>
          <div className="mb-1 flex items-center gap-2 text-xs text-[var(--color-text-tertiary)]">
            <Network className="h-3.5 w-3.5" />
            <span>UDP 监听 ({udp.length})</span>
          </div>
          <div className="divide-y divide-[var(--color-border-secondary)]">
            {udp.map((p) => renderPort(p, false))}
          </div>
        </div>
      )}
      {tcp.length === 0 && udp.length === 0 && (
        <p className="py-4 text-center text-sm text-[var(--color-text-tertiary)]">无监听端口</p>
      )}
    </div>
  )
}
