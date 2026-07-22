// REQ-U-005/REQ-U-009: 服务配置可视化表单
// REQ-2.10: 可视化表单包含所有 service.yaml 字段
// 用于创建服务和编辑服务配置

import { useState, useMemo, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiGet } from '@/lib/api-client'
import { z } from 'zod'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Button } from '@/components/ui/Button'
import { IconPicker } from '@/components/common/IconPicker'
import { ChevronDown, ChevronRight } from 'lucide-react'

// ===== 类型定义（对应 service.yaml 结构，REQ-2.3.2）=====

export interface ReadinessConfig {
  type: 'fd_notify' | 'tcp_check' | 'http_check' | 'script'
  fd?: number
  port?: number
  url?: string
  expected_status?: number
  check?: string[]
  interval_seconds?: number
  timeout_seconds?: number
}

export interface RestartConfig {
  policy: 'always' | 'on-failure' | 'never'
  backoff_ms?: number
  max_backoff_ms?: number
  multiplier?: number
  max_retries?: number
  reset_after_seconds?: number
}

export interface StopConfig {
  grace_seconds?: number
  timeout_seconds?: number
}

export interface LoggingConfig {
  enabled?: boolean
  max_size_mb?: number
  max_files?: number
}

export interface SignalsConfig {
  reload?: string
  rotate_logs?: string
  graceful_quit?: string
}

export interface ServiceConfig {
  name: string
  version?: string
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
  readiness?: ReadinessConfig
  restart?: RestartConfig
  stop?: StopConfig
  logging?: LoggingConfig
  signals?: SignalsConfig
}

export interface ServiceFormProps {
  initial?: Partial<ServiceConfig>
  onSubmit: (config: ServiceConfig) => void
  onCancel?: () => void
  submitLabel?: string
  isLoading?: boolean
}

// ===== YAML 序列化/解析工具 =====

/** 将字符串转为 YAML 安全格式 */
function yamlStr(s: string): string {
  if (s === '' || /[\s#:{}\[\],&*!|>'"%@`]/.test(s) || /^\d/.test(s) || /^(true|false|null|yes|no|on|off)$/i.test(s)) {
    return `"${s.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
  }
  return s
}

/** 解析 YAML 标量值 */
function parseYamlValue(value: string): string | number | boolean | string[] {
  const v = value.trim()
  if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) {
    return v.slice(1, -1)
  }
  if (v === 'true') return true
  if (v === 'false') return false
  if (/^-?\d+$/.test(v)) return parseInt(v, 10)
  if (/^-?\d+\.\d+$/.test(v)) return parseFloat(v)
  if (v.startsWith('[') && v.endsWith(']')) {
    const inner = v.slice(1, -1).trim()
    if (!inner) return []
    return inner.split(',').map((s) => String(parseYamlValue(s.trim())))
  }
  return v
}

/** 序列化 ServiceConfig 为 YAML 字符串 */
export function serializeServiceConfig(config: ServiceConfig): string {
  const lines: string[] = []

  lines.push(`name: ${yamlStr(config.name)}`)
  if (config.version) lines.push(`version: ${yamlStr(config.version)}`)
  if (config.description) lines.push(`description: ${yamlStr(config.description)}`)
  if (config.icon) lines.push(`icon: ${yamlStr(config.icon)}`)
  if (config.autostart !== undefined) lines.push(`autostart: ${config.autostart}`)

  if (config.command?.length) {
    lines.push('command:')
    for (const c of config.command) lines.push(`  - ${yamlStr(c)}`)
  }

  if (config.runtime) lines.push(`runtime: ${yamlStr(config.runtime)}`)
  if (config.user) lines.push(`user: ${yamlStr(config.user)}`)
  if (config.group) lines.push(`group: ${yamlStr(config.group)}`)
  if (config.workdir) lines.push(`workdir: ${yamlStr(config.workdir)}`)

  if (config.depends_on?.length) {
    lines.push('depends_on:')
    for (const d of config.depends_on) lines.push(`  - ${yamlStr(d)}`)
  }
  if (config.tags?.length) {
    lines.push('tags:')
    for (const tag of config.tags) lines.push(`  - ${yamlStr(tag)}`)
  }

  if (config.readiness) {
    const r = config.readiness
    lines.push('readiness:')
    lines.push(`  type: ${r.type}`)
    if (r.fd !== undefined) lines.push(`  fd: ${r.fd}`)
    if (r.port !== undefined) lines.push(`  port: ${r.port}`)
    if (r.url) lines.push(`  url: ${yamlStr(r.url)}`)
    if (r.expected_status !== undefined) lines.push(`  expected_status: ${r.expected_status}`)
    if (r.check?.length) {
      lines.push('  check:')
      for (const c of r.check) lines.push(`    - ${yamlStr(c)}`)
    }
    if (r.interval_seconds !== undefined) lines.push(`  interval_seconds: ${r.interval_seconds}`)
    if (r.timeout_seconds !== undefined) lines.push(`  timeout_seconds: ${r.timeout_seconds}`)
  }

  if (config.restart) {
    const r = config.restart
    lines.push('restart:')
    lines.push(`  policy: ${r.policy}`)
    if (r.backoff_ms !== undefined) lines.push(`  backoff_ms: ${r.backoff_ms}`)
    if (r.max_backoff_ms !== undefined) lines.push(`  max_backoff_ms: ${r.max_backoff_ms}`)
    if (r.multiplier !== undefined) lines.push(`  multiplier: ${r.multiplier}`)
    if (r.max_retries !== undefined) lines.push(`  max_retries: ${r.max_retries}`)
    if (r.reset_after_seconds !== undefined) lines.push(`  reset_after_seconds: ${r.reset_after_seconds}`)
  }

  if (config.stop) {
    const s = config.stop
    lines.push('stop:')
    if (s.grace_seconds !== undefined) lines.push(`  grace_seconds: ${s.grace_seconds}`)
    if (s.timeout_seconds !== undefined) lines.push(`  timeout_seconds: ${s.timeout_seconds}`)
  }

  if (config.logging) {
    const l = config.logging
    lines.push('logging:')
    if (l.enabled !== undefined) lines.push(`  enabled: ${l.enabled}`)
    if (l.max_size_mb !== undefined) lines.push(`  max_size_mb: ${l.max_size_mb}`)
    if (l.max_files !== undefined) lines.push(`  max_files: ${l.max_files}`)
  }

  if (config.signals) {
    const s = config.signals
    lines.push('signals:')
    if (s.reload) lines.push(`  reload: ${yamlStr(s.reload)}`)
    if (s.rotate_logs) lines.push(`  rotate_logs: ${yamlStr(s.rotate_logs)}`)
    if (s.graceful_quit) lines.push(`  graceful_quit: ${yamlStr(s.graceful_quit)}`)
  }

  return lines.join('\n') + '\n'
}

/** 解析 service.yaml 字符串为 Partial<ServiceConfig> */
export function parseServiceYaml(yaml: string): Partial<ServiceConfig> {
  const result: Record<string, unknown> = {}
  const lines = yaml.split('\n')
  let i = 0

  while (i < lines.length) {
    const raw = lines[i]
    if (!raw) { i++; continue }
    const hashIdx = raw.indexOf('#')
    const line = hashIdx >= 0 ? raw.slice(0, hashIdx) : raw
    const trimmed = line.trim()

    if (!trimmed) { i++; continue }
    // 只处理顶层（无缩进）
    if (line.startsWith(' ') || line.startsWith('\t')) { i++; continue }

    const colonIdx = trimmed.indexOf(':')
    if (colonIdx < 0) { i++; continue }

    const key = trimmed.slice(0, colonIdx).trim()
    const value = trimmed.slice(colonIdx + 1).trim()

    if (value) {
      result[key] = parseYamlValue(value)
      i++
    } else {
      // 收集缩进行
      const blockLines: string[] = []
      i++
      while (i < lines.length) {
        const bLine = lines[i]
        if (!bLine) { i++; continue }
        if (bLine.trim() === '' || bLine.trim().startsWith('#')) { i++; continue }
        if (bLine.startsWith(' ') || bLine.startsWith('\t')) {
          blockLines.push(bLine)
          i++
        } else {
          break
        }
      }

      const firstBlock = blockLines[0]
      if (firstBlock && firstBlock.trim().startsWith('-')) {
        result[key] = blockLines.map((l) => parseYamlValue(l.trim().replace(/^-\s*/, '')))
      } else {
        result[key] = parseNestedObject(blockLines)
      }
    }
  }

  return result as unknown as Partial<ServiceConfig>
}

/** 解析嵌套对象（readiness/restart/stop/logging/signals） */
function parseNestedObject(lines: string[]): Record<string, unknown> {
  const obj: Record<string, unknown> = {}
  let i = 0

  while (i < lines.length) {
    const raw = lines[i]
    if (!raw) { i++; continue }
    const hashIdx = raw.indexOf('#')
    const line = hashIdx >= 0 ? raw.slice(0, hashIdx) : raw
    const trimmed = line.trim()

    if (!trimmed) { i++; continue }

    const colonIdx = trimmed.indexOf(':')
    if (colonIdx < 0) { i++; continue }

    const key = trimmed.slice(0, colonIdx).trim()
    const value = trimmed.slice(colonIdx + 1).trim()

    if (value) {
      obj[key] = parseYamlValue(value)
      i++
    } else {
      // 子数组（如 readiness.check）
      const currentIndent = line.length - line.trimStart().length
      const subBlock: string[] = []
      i++
      while (i < lines.length) {
        const sLine = lines[i]
        if (!sLine) { i++; continue }
        if (sLine.trim() === '') { i++; continue }
        const sIndent = sLine.length - sLine.trimStart().length
        if (sIndent > currentIndent) {
          subBlock.push(sLine)
          i++
        } else {
          break
        }
      }
      const firstSub = subBlock[0]
      if (firstSub && firstSub.trim().startsWith('-')) {
        obj[key] = subBlock.map((l) => parseYamlValue(l.trim().replace(/^-\s*/, '')))
      }
    }
  }

  return obj
}

// ===== 表单状态 =====

interface FormState {
  // 基本信息
  name: string
  version: string
  description: string
  icon: string
  tags: string
  // 启动配置
  command: string
  workdir: string
  runtime: string
  autostart: boolean
  user: string
  group: string
  depends_on: string
  // 就绪检查
  readinessType: string
  readinessFd: string
  readinessPort: string
  readinessUrl: string
  readinessExpectedStatus: string
  readinessCheck: string
  readinessInterval: string
  readinessTimeout: string
  // 重启策略
  restartPolicy: string
  backoffMs: string
  maxBackoffMs: string
  multiplier: string
  maxRetries: string
  resetAfterSeconds: string
  // 停止配置
  graceSeconds: string
  stopTimeoutSeconds: string
  // 日志配置
  loggingEnabled: boolean
  maxSizeMb: string
  maxFiles: string
  // 信号配置
  signalReload: string
  signalRotateLogs: string
  signalGracefulQuit: string
}

function defaultFormState(): FormState {
  return {
    name: '',
    version: '1.0.0',
    description: '',
    icon: 'box',
    tags: '',
    command: '',
    workdir: '',
    runtime: '',
    autostart: true,
    user: '',
    group: '',
    depends_on: '',
    readinessType: 'none',
    readinessFd: '3',
    readinessPort: '80',
    readinessUrl: '',
    readinessExpectedStatus: '200',
    readinessCheck: '',
    readinessInterval: '1',
    readinessTimeout: '5',
    restartPolicy: 'default',
    backoffMs: '1000',
    maxBackoffMs: '30000',
    multiplier: '2',
    maxRetries: '0',
    resetAfterSeconds: '300',
    graceSeconds: '10',
    stopTimeoutSeconds: '60',
    loggingEnabled: true,
    maxSizeMb: '10',
    maxFiles: '5',
    signalReload: '',
    signalRotateLogs: '',
    signalGracefulQuit: '',
  }
}

function formStateFromConfig(config?: Partial<ServiceConfig>): FormState {
  const base = defaultFormState()
  if (!config) return base
  return {
    ...base,
    name: config.name ?? '',
    version: config.version ?? '1.0.0',
    description: config.description ?? '',
    icon: config.icon ?? 'box',
    tags: config.tags?.join(', ') ?? '',
    command: config.command?.join(' ') ?? '',
    workdir: config.workdir ?? '',
    runtime: config.runtime ?? '',
    autostart: config.autostart ?? true,
    user: config.user ?? '',
    group: config.group ?? '',
    depends_on: config.depends_on?.join(', ') ?? '',
    readinessType: config.readiness?.type ?? 'none',
    readinessFd: config.readiness?.fd?.toString() ?? '3',
    readinessPort: config.readiness?.port?.toString() ?? '80',
    readinessUrl: config.readiness?.url ?? '',
    readinessExpectedStatus: config.readiness?.expected_status?.toString() ?? '200',
    readinessCheck: config.readiness?.check?.join(' ') ?? '',
    readinessInterval: config.readiness?.interval_seconds?.toString() ?? '1',
    readinessTimeout: config.readiness?.timeout_seconds?.toString() ?? '5',
    restartPolicy: config.restart?.policy ?? 'default',
    backoffMs: config.restart?.backoff_ms?.toString() ?? '1000',
    maxBackoffMs: config.restart?.max_backoff_ms?.toString() ?? '30000',
    multiplier: config.restart?.multiplier?.toString() ?? '2',
    maxRetries: config.restart?.max_retries?.toString() ?? '0',
    resetAfterSeconds: config.restart?.reset_after_seconds?.toString() ?? '300',
    graceSeconds: config.stop?.grace_seconds?.toString() ?? '10',
    stopTimeoutSeconds: config.stop?.timeout_seconds?.toString() ?? '60',
    loggingEnabled: config.logging?.enabled ?? true,
    maxSizeMb: config.logging?.max_size_mb?.toString() ?? '10',
    maxFiles: config.logging?.max_files?.toString() ?? '5',
    signalReload: config.signals?.reload ?? '',
    signalRotateLogs: config.signals?.rotate_logs ?? '',
    signalGracefulQuit: config.signals?.graceful_quit ?? '',
  }
}

function toInt(s: string): number | undefined {
  if (s === '') return undefined
  const n = parseInt(s, 10)
  return isNaN(n) ? undefined : n
}

// ===== E-03-001: zod 表单校验 =====

/** 数值字符串校验：空字符串合法（使用默认值），非空时需为 [min, max] 内的整数 */
function intStr(min: number, max: number, label: string) {
  return z.string().refine(
    (v) => {
      if (v === '') return true
      const n = Number(v)
      return Number.isInteger(n) && n >= min && n <= max
    },
    `${label}需为 ${min}-${max} 的整数`,
  )
}

const serviceFormSchema = z.object({
  name: z
    .string()
    .min(1, '服务名称不能为空')
    .regex(/^[a-z][a-z0-9-]*$/, '服务名称需匹配 ^[a-z][a-z0-9-]*$（小写字母开头，仅含小写字母、数字、连字符）'),
  command: z.string().min(1, '启动命令不能为空'),
  readinessFd: intStr(0, 65535, 'FD '),
  readinessPort: intStr(1, 65535, '端口 '),
  readinessExpectedStatus: intStr(100, 599, '期望状态码 '),
  readinessInterval: intStr(1, 3600, '检查间隔 '),
  readinessTimeout: intStr(1, 3600, '就绪超时 '),
  backoffMs: intStr(0, 3_600_000, '退避起始 '),
  maxBackoffMs: intStr(0, 3_600_000, '退避上限 '),
  multiplier: intStr(1, 100, '倍数 '),
  maxRetries: intStr(0, 1_000_000, '最大重试 '),
  resetAfterSeconds: intStr(1, 86400, '重置间隔 '),
  graceSeconds: intStr(1, 3600, '优雅等待 '),
  stopTimeoutSeconds: intStr(1, 3600, '停止超时 '),
  maxSizeMb: intStr(1, 10240, '单文件上限 '),
  maxFiles: intStr(1, 1000, '保留文件数 '),
})

type FormErrors = Partial<Record<keyof FormState, string>>

/** 将表单状态转为 ServiceConfig */
function buildConfig(form: FormState): ServiceConfig {
  const config: ServiceConfig = {
    name: form.name.trim(),
    command: form.command.trim().split(/\s+/).filter(Boolean),
  }

  if (form.version.trim()) config.version = form.version.trim()
  if (form.description.trim()) config.description = form.description.trim()
  if (form.icon) config.icon = form.icon
  config.autostart = form.autostart
  if (form.workdir.trim()) config.workdir = form.workdir.trim()
  if (form.runtime.trim()) config.runtime = form.runtime.trim()
  if (form.user.trim()) config.user = form.user.trim()
  if (form.group.trim()) config.group = form.group.trim()

  const tags = form.tags.split(',').map((s) => s.trim()).filter(Boolean)
  if (tags.length) config.tags = tags
  const deps = form.depends_on.split(',').map((s) => s.trim()).filter(Boolean)
  if (deps.length) config.depends_on = deps

  // readiness
  if (form.readinessType !== 'none') {
    const r: ReadinessConfig = { type: form.readinessType as ReadinessConfig['type'] }
    if (form.readinessType === 'fd_notify') {
      const fd = toInt(form.readinessFd)
      if (fd !== undefined) r.fd = fd
    }
    if (form.readinessType === 'tcp_check') {
      const port = toInt(form.readinessPort)
      if (port !== undefined) r.port = port
    }
    if (form.readinessType === 'http_check') {
      if (form.readinessUrl.trim()) r.url = form.readinessUrl.trim()
      const status = toInt(form.readinessExpectedStatus)
      if (status !== undefined) r.expected_status = status
    }
    if (form.readinessType === 'script' && form.readinessCheck.trim()) {
      r.check = form.readinessCheck.trim().split(/\s+/).filter(Boolean)
    }
    const interval = toInt(form.readinessInterval)
    if (interval !== undefined) r.interval_seconds = interval
    const timeout = toInt(form.readinessTimeout)
    if (timeout !== undefined) r.timeout_seconds = timeout
    config.readiness = r
  }

  // restart
  if (form.restartPolicy !== 'default' && form.restartPolicy) {
    const r: RestartConfig = { policy: form.restartPolicy as RestartConfig['policy'] }
    const v = toInt(form.backoffMs); if (v !== undefined) r.backoff_ms = v
    const v2 = toInt(form.maxBackoffMs); if (v2 !== undefined) r.max_backoff_ms = v2
    const v3 = toInt(form.multiplier); if (v3 !== undefined) r.multiplier = v3
    const v4 = toInt(form.maxRetries); if (v4 !== undefined) r.max_retries = v4
    const v5 = toInt(form.resetAfterSeconds); if (v5 !== undefined) r.reset_after_seconds = v5
    config.restart = r
  }

  // stop
  const gs = toInt(form.graceSeconds)
  const ts = toInt(form.stopTimeoutSeconds)
  if (gs !== undefined || ts !== undefined) {
    config.stop = {}
    if (gs !== undefined) config.stop.grace_seconds = gs
    if (ts !== undefined) config.stop.timeout_seconds = ts
  }

  // logging
  config.logging = { enabled: form.loggingEnabled }
  const ms = toInt(form.maxSizeMb); if (ms !== undefined) config.logging.max_size_mb = ms
  const mf = toInt(form.maxFiles); if (mf !== undefined) config.logging.max_files = mf

  // signals
  if (form.signalReload.trim() || form.signalRotateLogs.trim() || form.signalGracefulQuit.trim()) {
    config.signals = {}
    if (form.signalReload.trim()) config.signals.reload = form.signalReload.trim()
    if (form.signalRotateLogs.trim()) config.signals.rotate_logs = form.signalRotateLogs.trim()
    if (form.signalGracefulQuit.trim()) config.signals.graceful_quit = form.signalGracefulQuit.trim()
  }

  return config
}

// ===== 下拉选项 =====

const READINESS_TYPE_OPTIONS = [
  { value: 'none', label: '无就绪检查' },
  { value: 'fd_notify', label: 'fd_notify（FD 通知）' },
  { value: 'tcp_check', label: 'tcp_check（TCP 检查）' },
  { value: 'http_check', label: 'http_check（HTTP 检查）' },
  { value: 'script', label: 'script（脚本检查）' },
]

const RESTART_POLICY_OPTIONS = [
  { value: 'default', label: '全局默认（不设置）' },
  { value: 'always', label: 'always（总是重启）' },
  { value: 'on-failure', label: 'on-failure（失败时重启）' },
  { value: 'never', label: 'never（从不重启）' },
]

// ===== 辅助组件 =====

function Field({
  label,
  required,
  hint,
  error,
  children,
}: {
  label: string
  required?: boolean
  hint?: string
  error?: string
  children: ReactNode
}) {
  return (
    <div>
      <label className="text-sm text-[var(--color-text-secondary)]">
        {label} {required && <span className="text-[var(--color-text-error)]">*</span>}
      </label>
      <div className="mt-1">{children}</div>
      {error ? (
        <p className="mt-1 text-xs text-[var(--color-text-error)]">{error}</p>
      ) : hint ? (
        <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">{hint}</p>
      ) : null}
    </div>
  )
}

function CollapsibleSection({
  title,
  defaultOpen = true,
  children,
}: {
  title: string
  defaultOpen?: boolean
  children: ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <Card>
      <CardHeader>
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex w-full items-center justify-between"
        >
          <CardTitle className="text-sm">{title}</CardTitle>
          {open ? <ChevronDown className="h-4 w-4 text-[var(--color-text-tertiary)]" /> : <ChevronRight className="h-4 w-4 text-[var(--color-text-tertiary)]" />}
        </button>
      </CardHeader>
      {open && <CardContent className="space-y-3">{children}</CardContent>}
    </Card>
  )
}

function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
}) {
  return (
    <div className="flex items-center gap-2">
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors ${checked ? 'bg-[var(--color-brand-primary)]' : 'bg-[var(--color-border-secondary)]'}`}
      >
        <span className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow-sm transition-transform ${checked ? 'translate-x-4' : 'translate-x-0'}`} />
      </button>
      <label className="text-sm text-[var(--color-text-secondary)]">{label}</label>
    </div>
  )
}

// ===== 主组件 =====

export function ServiceForm({ initial, onSubmit, onCancel, submitLabel = '提交', isLoading }: ServiceFormProps) {
  const [form, setForm] = useState<FormState>(() => formStateFromConfig(initial))
  // E-03-001: 字段级校验错误状态
  const [errors, setErrors] = useState<FormErrors>({})

  // 运行时列表从 /api/runtimes 动态获取（含兜底内置）
  const { data: runtimesData } = useQuery({
    queryKey: ['runtimes'],
    queryFn: () => apiGet<{ runtimes: Array<{ alias: string; available: boolean }> }>('/api/runtimes'),
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
    // 当前值不在列表中时追加（如自定义路径）
    if (form.runtime && !seen.has(form.runtime)) {
      opts.push({ value: form.runtime, label: form.runtime })
    }
    return opts
  }, [runtimesData, form.runtime])

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((f) => ({ ...f, [key]: value }))
    // 字段被编辑后清除其错误提示
    setErrors((prev) => {
      if (!prev[key]) return prev
      const next = { ...prev }
      delete next[key]
      return next
    })
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const result = serviceFormSchema.safeParse(form)
    if (!result.success) {
      const fieldErrors: FormErrors = {}
      for (const issue of result.error.issues) {
        const key = issue.path[0] as keyof FormState
        if (!fieldErrors[key]) {
          fieldErrors[key] = issue.message
        }
      }
      setErrors(fieldErrors)
      return
    }
    setErrors({})
    onSubmit(buildConfig(form))
  }

  const isValid = form.name.trim() !== '' && form.command.trim() !== ''

  return (
    <form onSubmit={handleSubmit} className="space-y-3">
      {/* 基本信息 */}
      <CollapsibleSection title="基本信息">
        <Field label="服务名称" required hint="正则 ^[a-z][a-z0-9-]*$" error={errors.name}>
          <Input
            value={form.name}
            onChange={(e) => set('name', e.target.value)}
            placeholder="my-service"
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="版本">
            <Input
              value={form.version}
              onChange={(e) => set('version', e.target.value)}
              placeholder="1.0.0"
            />
          </Field>
          <Field label="图标">
            <IconPicker
              value={form.icon}
              onChange={(v) => set('icon', v)}
            />
          </Field>
        </div>
        <Field label="描述">
          <Input
            value={form.description}
            onChange={(e) => set('description', e.target.value)}
            placeholder="服务描述（可选）"
          />
        </Field>
        <Field label="标签" hint="逗号分隔">
          <Input
            value={form.tags}
            onChange={(e) => set('tags', e.target.value)}
            placeholder="web, api, backend"
          />
        </Field>
      </CollapsibleSection>

      {/* 启动配置 */}
      <CollapsibleSection title="启动配置">
        <Field label="启动命令" required hint="命令将以空格分割为数组；含空格的路径或参数请保存后切换到 YAML 编辑模式直接编辑 command 数组" error={errors.command}>
          <Input
            value={form.command}
            onChange={(e) => set('command', e.target.value)}
            placeholder="/usr/bin/myapp --flag arg"
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="工作目录">
            <Input
              value={form.workdir}
              onChange={(e) => set('workdir', e.target.value)}
              placeholder="/etc/supd/services/<name>"
            />
          </Field>
          <Field label="运行时" hint="bash / sh / python3 / node 或可执行文件绝对路径">
            <Input
              value={form.runtime}
              onChange={(e) => set('runtime', e.target.value)}
              placeholder="bash / sh / python3 / node 或绝对路径"
              list="service-form-runtime-options"
            />
            <datalist id="service-form-runtime-options">
              {runtimeOptions.filter((o) => o.value).map((o) => (
                <option key={o.value} value={o.value} />
              ))}
            </datalist>
          </Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field label="运行用户">
            <Input
              value={form.user}
              onChange={(e) => set('user', e.target.value)}
              placeholder="root"
            />
          </Field>
          <Field label="用户组">
            <Input
              value={form.group}
              onChange={(e) => set('group', e.target.value)}
              placeholder="root"
            />
          </Field>
        </div>
        <Field label="依赖服务" hint="逗号分隔">
          <Input
            value={form.depends_on}
            onChange={(e) => set('depends_on', e.target.value)}
            placeholder="service1, service2"
          />
        </Field>
        <Toggle
          checked={form.autostart}
          onChange={(v) => set('autostart', v)}
          label="开机自启（autostart）"
        />
      </CollapsibleSection>

      {/* 就绪检查 */}
      <CollapsibleSection title="就绪检查（readiness）" defaultOpen={false}>
        <Field label="检查类型">
          <Select
            options={READINESS_TYPE_OPTIONS}
            value={form.readinessType}
            onChange={(e) => set('readinessType', e.target.value)}
            placeholder=""
          />
        </Field>

        {form.readinessType === 'fd_notify' && (
          <Field label="FD（文件描述符）" error={errors.readinessFd}>
            <Input
              type="number"
              value={form.readinessFd}
              onChange={(e) => set('readinessFd', e.target.value)}
              placeholder="3"
            />
          </Field>
        )}

        {form.readinessType === 'tcp_check' && (
          <Field label="端口（port）" error={errors.readinessPort}>
            <Input
              type="number"
              value={form.readinessPort}
              onChange={(e) => set('readinessPort', e.target.value)}
              placeholder="80"
            />
          </Field>
        )}

        {form.readinessType === 'http_check' && (
          <>
            <Field label="URL">
              <Input
                value={form.readinessUrl}
                onChange={(e) => set('readinessUrl', e.target.value)}
                placeholder="http://localhost:7979/health"
              />
            </Field>
            <Field label="期望状态码（expected_status）" error={errors.readinessExpectedStatus}>
              <Input
                type="number"
                value={form.readinessExpectedStatus}
                onChange={(e) => set('readinessExpectedStatus', e.target.value)}
                placeholder="200"
              />
            </Field>
          </>
        )}

        {form.readinessType === 'script' && (
          <Field label="检查命令（check）" hint="空格分割为数组">
            <Input
              value={form.readinessCheck}
              onChange={(e) => set('readinessCheck', e.target.value)}
              placeholder="/usr/bin/check.sh --flag"
            />
          </Field>
        )}

        {form.readinessType !== 'none' && (
          <div className="grid grid-cols-2 gap-3">
            <Field label="检查间隔（interval_seconds，默认1）" error={errors.readinessInterval}>
              <Input
                type="number"
                value={form.readinessInterval}
                onChange={(e) => set('readinessInterval', e.target.value)}
                placeholder="1"
              />
            </Field>
            <Field label="超时（timeout_seconds，默认5）" error={errors.readinessTimeout}>
              <Input
                type="number"
                value={form.readinessTimeout}
                onChange={(e) => set('readinessTimeout', e.target.value)}
                placeholder="5"
              />
            </Field>
          </div>
        )}
      </CollapsibleSection>

      {/* 重启策略 */}
      <CollapsibleSection title="重启策略（restart）" defaultOpen={false}>
        <Field label="策略（policy）">
          <Select
            options={RESTART_POLICY_OPTIONS}
            value={form.restartPolicy}
            onChange={(e) => set('restartPolicy', e.target.value)}
            placeholder=""
          />
        </Field>
        {form.restartPolicy !== 'default' && (
          <>
            <div className="grid grid-cols-2 gap-3">
              <Field label="退避起始（backoff_ms，默认1000）" error={errors.backoffMs}>
                <Input
                  type="number"
                  value={form.backoffMs}
                  onChange={(e) => set('backoffMs', e.target.value)}
                  placeholder="1000"
                />
              </Field>
              <Field label="退避上限（max_backoff_ms，默认30000）" error={errors.maxBackoffMs}>
                <Input
                  type="number"
                  value={form.maxBackoffMs}
                  onChange={(e) => set('maxBackoffMs', e.target.value)}
                  placeholder="30000"
                />
              </Field>
            </div>
            <div className="grid grid-cols-3 gap-3">
              <Field label="倍数（multiplier，默认2）" error={errors.multiplier}>
                <Input
                  type="number"
                  value={form.multiplier}
                  onChange={(e) => set('multiplier', e.target.value)}
                  placeholder="2"
                />
              </Field>
              <Field label="最大重试（max_retries，默认0=无限）" error={errors.maxRetries}>
                <Input
                  type="number"
                  value={form.maxRetries}
                  onChange={(e) => set('maxRetries', e.target.value)}
                  placeholder="0"
                />
              </Field>
              <Field label="重置间隔（reset_after_seconds，默认300）" error={errors.resetAfterSeconds}>
                <Input
                  type="number"
                  value={form.resetAfterSeconds}
                  onChange={(e) => set('resetAfterSeconds', e.target.value)}
                  placeholder="300"
                />
              </Field>
            </div>
          </>
        )}
      </CollapsibleSection>

      {/* 停止配置 */}
      <CollapsibleSection title="停止配置（stop）" defaultOpen={false}>
        <div className="grid grid-cols-2 gap-3">
          <Field label="优雅等待（grace_seconds，默认10）" error={errors.graceSeconds}>
            <Input
              type="number"
              value={form.graceSeconds}
              onChange={(e) => set('graceSeconds', e.target.value)}
              placeholder="10"
            />
          </Field>
          <Field label="停止超时（timeout_seconds，默认60）" error={errors.stopTimeoutSeconds}>
            <Input
              type="number"
              value={form.stopTimeoutSeconds}
              onChange={(e) => set('stopTimeoutSeconds', e.target.value)}
              placeholder="60"
            />
          </Field>
        </div>
      </CollapsibleSection>

      {/* 日志配置 */}
      <CollapsibleSection title="日志配置（logging）" defaultOpen={false}>
        <Toggle
          checked={form.loggingEnabled}
          onChange={(v) => set('loggingEnabled', v)}
          label="启用日志（enabled）"
        />
        <div className="grid grid-cols-2 gap-3">
          <Field label="单文件上限（max_size_mb，默认10）" error={errors.maxSizeMb}>
            <Input
              type="number"
              value={form.maxSizeMb}
              onChange={(e) => set('maxSizeMb', e.target.value)}
              placeholder="10"
            />
          </Field>
          <Field label="保留文件数（max_files，默认5）" error={errors.maxFiles}>
            <Input
              type="number"
              value={form.maxFiles}
              onChange={(e) => set('maxFiles', e.target.value)}
              placeholder="5"
            />
          </Field>
        </div>
      </CollapsibleSection>

      {/* 信号配置 */}
      <CollapsibleSection title="信号配置（signals）" defaultOpen={false}>
        <div className="grid grid-cols-3 gap-3">
          <Field label="reload" hint="如 HUP">
            <Input
              value={form.signalReload}
              onChange={(e) => set('signalReload', e.target.value)}
              placeholder="HUP"
            />
          </Field>
          <Field label="rotate_logs" hint="如 USR1">
            <Input
              value={form.signalRotateLogs}
              onChange={(e) => set('signalRotateLogs', e.target.value)}
              placeholder="USR1"
            />
          </Field>
          <Field label="graceful_quit" hint="如 QUIT">
            <Input
              value={form.signalGracefulQuit}
              onChange={(e) => set('signalGracefulQuit', e.target.value)}
              placeholder="QUIT"
            />
          </Field>
        </div>
      </CollapsibleSection>

      {/* 提交按钮 */}
      <div className="flex justify-end gap-2 pt-2">
        {onCancel && (
          <Button type="button" variant="default" disabled={isLoading} onClick={onCancel}>
            取消
          </Button>
        )}
        <Button type="submit" variant="primary" disabled={!isValid || isLoading}>
          {isLoading ? '提交中...' : submitLabel}
        </Button>
      </div>
    </form>
  )
}
