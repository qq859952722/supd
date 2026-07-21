// REQ-U-011, REQ-2.9.10: 设置页面
// 单页滚动 + 左侧分段锚点导航（不用 tab），覆盖 config.yaml 全部字段

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPut, apiPost, apiDelete } from '@/lib/api-client'
import { toast } from '@/components/ui/Toast'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { SkeletonCard } from '@/components/ui/Skeleton'
import { TokenManager } from '@/components/settings/TokenManager'
import { EnvEditor } from '@/components/settings/EnvEditor'
import { t } from '@/lib/i18n'
import { Save, Plus, Trash2, Info, AlertTriangle, Upload, Loader2, RefreshCw } from 'lucide-react'
import { useState, useEffect, useRef } from 'react'
import { getErrorMessage } from '@/lib/error-utils'

interface RestartDefaults {
  policy: string
  backoff_ms: number
  max_backoff_ms: number
  multiplier: number
  max_retries: number
  reset_after_seconds: number
}

interface AboutInfo {
  version: string
  build_time: string
  work_dir: string
}

interface SettingsData {
  http_listen: string
  auth_mode: string
  auth_token_configured?: boolean
  local_networks: string[]
  log_level: string
  log_max_size_mb: number
  log_max_files: number
  shutdown_grace_seconds: number
  extension_default_timeout_seconds: number
  extension_hard_limit_seconds: number
  run_history_retention_seconds: number
  file_history_versions: number
  max_upload_size_mb: number
  env_files?: string[]
  extension_dirs?: string[]
  defaults?: {
    restart?: RestartDefaults
  }
}

// 左侧锚点导航分段
const sections = [
  { id: 'general', label: '基本设置' },
  { id: 'log', label: '日志设置' },
  { id: 'shutdown', label: '关闭与扩展' },
  { id: 'upload', label: '文件上传' },
  { id: 'restart', label: '默认重启策略' },
  { id: 'envfiles', label: '环境变量文件' },
  { id: 'extdirs', label: '扩展目录' },
  { id: 'token', label: 'Token 管理' },
  { id: 'env', label: '全局环境变量' },
  { id: 'runtime', label: '运行时配置' },
  { id: 'about', label: '关于' },
]

// R-009 修复：重载配置按钮，调用 POST /api/reload 触发热重载
interface ReloadResponse {
  status: string
  message: string
  services: number
  global_extensions: number
  scan_errors: number
  error_details?: Array<{ path: string; message: string }>
}

function ReloadConfigButton() {
  const queryClient = useQueryClient()
  const reloadMutation = useMutation({
    mutationFn: () => apiPost<ReloadResponse>('/api/reload'),
    onSuccess: (data) => {
      if (data.status === 'partial') {
        toast.warning(`配置重载完成（部分错误）：${data.scan_errors} 个错误`)
      } else {
        toast.success(`配置已重载（${data.services} 个服务，${data.global_extensions} 个全局扩展）`)
      }
      // 刷新所有相关查询
      queryClient.invalidateQueries({ queryKey: ['settings'] })
      queryClient.invalidateQueries({ queryKey: ['services'] })
      queryClient.invalidateQueries({ queryKey: ['extensions'] })
    },
    onError: (err: unknown) => {
      toast.error(getErrorMessage(err, '配置重载失败'))
    },
  })

  return (
    <Button
      variant="default"
      size="sm"
      onClick={() => reloadMutation.mutate()}
      disabled={reloadMutation.isPending}
    >
      {reloadMutation.isPending ? (
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
      ) : (
        <RefreshCw className="h-3.5 w-3.5" />
      )}
      重载配置
    </Button>
  )
}

export default function SettingsPage() {
  const queryClient = useQueryClient()
  const [formState, setFormState] = useState<Partial<SettingsData>>({})
  const [initialized, setInitialized] = useState(false)
  const [activeSection, setActiveSection] = useState('general')
  const sectionRefs = useRef<Record<string, HTMLDivElement | null>>({})

  // E-01-003 修复：添加 isError 分支
  const { data, isLoading, isError } = useQuery({
    queryKey: ['settings'],
    queryFn: () => apiGet<SettingsData>('/api/settings'),
  })

  // 系统状态（用于"关于"区块）
  const { data: systemStatus } = useQuery({
    queryKey: ['system-status'],
    queryFn: () => apiGet<{ version: string; start_time: string; uptime_seconds: number; http_listen: string; auth_mode: string; work_dir: string }>('/api/system/status'),
  })

  if (data && !initialized) {
    setFormState(data)
    setInitialized(true)
  }

  const saveMutation = useMutation({
    mutationFn: (settings: Partial<SettingsData>) => apiPut('/api/settings', settings, true),
    onSuccess: () => {
      // REQ-2.9.10: 某些字段改动需重启 supd，保存时提示
      // F-06-001 修复：log_level/log_max_size_mb/log_max_files 在启动时应用，修改后需重启生效
      const needsRestart = ['http_listen', 'auth_mode', 'local_networks', 'log_level', 'log_max_size_mb', 'log_max_files'].some(
        (k) => JSON.stringify((formState as Record<string, unknown>)[k]) !== JSON.stringify((data as unknown as Record<string, unknown> | undefined)?.[k])
      )
      if (needsRestart) {
        toast.warning('部分设置需重启 supd 后生效')
      } else {
        toast.success('设置已保存')
      }
      queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (err: unknown) => {
      // F-06-001 修复：显示后端返回的具体错误信息
      const msg = getErrorMessage(err, '设置保存失败')
      toast.error(msg)
      // F-06-009 修复：保存失败时回滚 formState 到最新的服务端数据
      // 否则用户再次点击保存会再次提交相同错误数据，且 UI 显示的"已修改"状态与实际不符
      if (data) {
        setFormState(data)
      }
    },
  })

  // E-03-002 修复：保存按钮禁用状态（避免用户重复点击触发多次保存请求）
  const isSaving = saveMutation.isPending

  // F-06-002 修复：防止切换到 always_token 但未配置 token 导致自锁定
  const handleSave = () => {
    if (formState.auth_mode === 'always_token' && !data?.auth_token_configured) {
      toast.warning('当前认证模式为 always_token 但尚未配置 Token，保存后将无法访问 API。请先在下方"Token 管理"生成 Token。')
      return
    }
    saveMutation.mutate(formState)
  }

  // 滚动监听高亮当前段 — 滚动容器是 <main>，非 window
  useEffect(() => {
    const scrollContainer = document.querySelector('main')
    if (!scrollContainer) return
    const handler = () => {
      const containerTop = scrollContainer.getBoundingClientRect().top
      for (const s of sections) {
        const el = sectionRefs.current[s.id]
        if (el) {
          const rect = el.getBoundingClientRect()
          if (rect.top - containerTop <= 120) {
            setActiveSection(s.id)
          }
        }
      }
    }
    scrollContainer.addEventListener('scroll', handler, { passive: true })
    return () => scrollContainer.removeEventListener('scroll', handler)
  }, [])

  const scrollToSection = (id: string) => {
    const el = sectionRefs.current[id]
    const scrollContainer = document.querySelector('main')
    if (el && scrollContainer) {
      const top = el.getBoundingClientRect().top - scrollContainer.getBoundingClientRect().top + scrollContainer.scrollTop - 80
      scrollContainer.scrollTo({ top, behavior: 'smooth' })
    }
  }

  if (isLoading) {
    return <SkeletonCard />
  }

  // E-01-003 修复：设置加载失败时显示错误提示，不再停留在骨架屏
  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 py-20">
        <AlertTriangle className="h-8 w-8 text-[var(--color-text-warning)]" />
        <p className="text-sm text-[var(--color-text-secondary)]">设置加载失败，请检查后端服务是否正常运行</p>
      </div>
    )
  }

  const authModeOptions = [
    { value: 'none', label: t.settings.authNone },
    { value: 'local_skip', label: t.settings.authLocalSkip },
    { value: 'always_token', label: t.settings.authAlwaysToken },
  ]

  const logLevelOptions = [
    { value: 'debug', label: 'Debug' },
    { value: 'info', label: 'Info' },
    { value: 'warn', label: 'Warn' },
    { value: 'error', label: 'Error' },
  ]

  const restartPolicyOptions = [
    { value: 'always', label: 'always' },
    { value: 'on-failure', label: 'on-failure' },
    { value: 'never', label: 'never' },
  ]

  const restartDefaults = formState.defaults?.restart ?? {
    policy: 'always',
    backoff_ms: 1000,
    max_backoff_ms: 30000,
    multiplier: 2,
    max_retries: 0,
    reset_after_seconds: 300,
  }

  const updateRestartDefaults = (patch: Partial<RestartDefaults>) => {
    setFormState({
      ...formState,
      defaults: {
        ...formState.defaults,
        restart: { ...restartDefaults, ...patch },
      },
    })
  }

  const aboutInfo: AboutInfo = {
    version: systemStatus?.version ?? '-',
    build_time: systemStatus?.start_time ?? '-',
    work_dir: systemStatus?.work_dir ?? '-',
  }

  return (
    <div className="flex gap-6">
      {/* 左侧分段锚点导航 — REQ-2.9.10: 不用 tab */}
      <nav className="sticky top-4 h-fit w-48 shrink-0">
        <ul className="space-y-1">
          {sections.map((s) => (
            <li key={s.id}>
              <button
                onClick={() => scrollToSection(s.id)}
                className={`w-full text-left rounded-md px-3 py-2 text-sm transition-colors ${
                  activeSection === s.id
                    ? 'bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)] font-medium'
                    : 'text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-secondary)]'
                }`}
              >
                {s.label}
              </button>
            </li>
          ))}
        </ul>
      </nav>

      {/* 右侧滚动内容 */}
      <div className="flex-1 min-w-0 space-y-8 max-w-3xl">
        <div className="flex items-center justify-between">
          <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">{t.settings.title}</h1>
          {/* R-009 修复：Settings 页加"重载配置"按钮，触发热重载 */}
          <ReloadConfigButton />
        </div>

        {/* 基础设置 */}
        <div ref={(el) => { sectionRefs.current.general = el }} style={{ scrollMarginTop: '80px' }}>
          <Card>
            <CardHeader>
              <CardTitle>{t.settings.general}</CardTitle>
              <CardDescription>{t.settings.generalDesc}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">HTTP 监听地址</label>
                <Input
                  value={formState.http_listen ?? ''}
                  onChange={(e) => setFormState({ ...formState, http_listen: e.target.value })}
                  placeholder="默认: :8080"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">supd Web 服务和 API 的监听地址，格式为 主机:端口</p>
                <p className="mt-0.5 text-xs text-[var(--color-text-warning)]">⚠ 修改后需重启 supd 生效</p>
              </div>
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">认证模式</label>
                <Select
                  options={authModeOptions}
                  value={formState.auth_mode ?? 'local_skip'}
                  onChange={(e) => setFormState({ ...formState, auth_mode: e.target.value })}
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">none=不认证，local_skip=本地网络跳过认证，always_token=始终需要 Token</p>
                <p className="mt-0.5 text-xs text-[var(--color-text-warning)]">⚠ 修改后需重启 supd 生效</p>
              </div>
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">本地网络白名单</label>
                <Input
                  value={(formState.local_networks ?? []).join(', ')}
                  onChange={(e) => setFormState({ ...formState, local_networks: e.target.value.split(',').map(s => s.trim()).filter(Boolean) })}
                  placeholder="默认: 192.168.0.0/16, 10.0.0.0/8"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">local_skip 模式下跳过认证的 CIDR 列表，多个用逗号分隔</p>
                <p className="mt-0.5 text-xs text-[var(--color-text-warning)]">⚠ 修改后需重启 supd 生效</p>
              </div>
              <Button variant="primary" size="sm" onClick={handleSave} disabled={isSaving}>
                {isSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t.settings.save}
              </Button>
            </CardContent>
          </Card>
        </div>

        {/* 日志设置 */}
        <div ref={(el) => { sectionRefs.current.log = el }} style={{ scrollMarginTop: '80px' }}>
          <Card>
            <CardHeader>
              <CardTitle>{t.settings.logSettings}</CardTitle>
              <CardDescription>{t.settings.logSettingsDesc}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">日志级别</label>
                <Select
                  options={logLevelOptions}
                  value={formState.log_level ?? 'info'}
                  onChange={(e) => setFormState({ ...formState, log_level: e.target.value })}
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">控制日志输出的详细程度：debug 最详细，error 仅错误</p>
              </div>
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">单个日志文件最大大小</label>
                <Input
                  type="number"
                  min={1}
                  max={1024}
                  value={formState.log_max_size_mb ?? 10}
                  onChange={(e) => setFormState({ ...formState, log_max_size_mb: Number(e.target.value) })}
                  placeholder="默认: 10"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">单位：MB。达到此大小后日志文件将轮转</p>
              </div>
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">日志文件最大数量</label>
                <Input
                  type="number"
                  min={1}
                  max={100}
                  value={formState.log_max_files ?? 7}
                  onChange={(e) => setFormState({ ...formState, log_max_files: Number(e.target.value) })}
                  placeholder="默认: 7"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">日志轮转后保留的文件数量，超出后最旧的文件将被删除</p>
              </div>
              <Button variant="primary" size="sm" onClick={handleSave} disabled={isSaving}>
                {isSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t.settings.save}
              </Button>
            </CardContent>
          </Card>
        </div>

        {/* 关机/扩展执行 */}
        <div ref={(el) => { sectionRefs.current.shutdown = el }} style={{ scrollMarginTop: '80px' }}>
          <Card>
            <CardHeader>
              <CardTitle>{t.settings.shutdownSettings}</CardTitle>
              <CardDescription>{t.settings.shutdownSettingsDesc}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">关闭宽限期</label>
                <Input
                  type="number"
                  min={1}
                  max={300}
                  value={formState.shutdown_grace_seconds ?? 30}
                  onChange={(e) => setFormState({ ...formState, shutdown_grace_seconds: Number(e.target.value) })}
                  placeholder="默认: 30"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">单位：秒。supd 关闭时等待服务停止的最大时间</p>
              </div>
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">扩展默认超时</label>
                <Input
                  type="number"
                  min={1}
                  max={1800}
                  value={formState.extension_default_timeout_seconds ?? 600}
                  onChange={(e) => setFormState({ ...formState, extension_default_timeout_seconds: Number(e.target.value) })}
                  placeholder="默认: 600"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">单位：秒。扩展执行未指定超时时的默认值</p>
              </div>
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">扩展硬性超时上限</label>
                <Input
                  type="number"
                  min={1}
                  max={3600}
                  value={formState.extension_hard_limit_seconds ?? 1800}
                  onChange={(e) => setFormState({ ...formState, extension_hard_limit_seconds: Number(e.target.value) })}
                  placeholder="默认: 1800"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">单位：秒。扩展执行的最大允许时间，不可超过此上限（30分钟）</p>
              </div>
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">运行历史保留时长</label>
                <Input
                  type="number"
                  min={60}
                  max={2592000}
                  value={formState.run_history_retention_seconds ?? 604800}
                  onChange={(e) => setFormState({ ...formState, run_history_retention_seconds: Number(e.target.value) })}
                  placeholder="默认: 604800"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">单位：秒。扩展运行历史记录的保留时长（604800秒 = 7天）</p>
              </div>
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">文件历史版本保留数</label>
                <Input
                  type="number"
                  min={1}
                  max={200}
                  value={formState.file_history_versions ?? 50}
                  onChange={(e) => setFormState({ ...formState, file_history_versions: Number(e.target.value) })}
                  placeholder="默认: 50"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">文件编辑历史保留的版本数量，超出后最旧版本将被删除</p>
              </div>
              <Button variant="primary" size="sm" onClick={handleSave} disabled={isSaving}>
                {isSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t.settings.save}
              </Button>
            </CardContent>
          </Card>
        </div>

        {/* 文件上传 */}
        <div ref={(el) => { sectionRefs.current.upload = el }} style={{ scrollMarginTop: '80px' }}>
          <Card>
            <CardHeader>
              <CardTitle>文件上传</CardTitle>
              <CardDescription>通过 Web 界面上传文件的大小限制</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">最大上传文件大小</label>
                <Input
                  type="number"
                  min={1}
                  max={1024}
                  value={formState.max_upload_size_mb ?? 100}
                  onChange={(e) => setFormState({ ...formState, max_upload_size_mb: Number(e.target.value) })}
                  placeholder="默认: 100"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">单位：MB。通过 Web 界面上传文件的大小上限</p>
              </div>
              <Button variant="primary" size="sm" onClick={handleSave} disabled={isSaving}>
                {isSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t.settings.save}
              </Button>
            </CardContent>
          </Card>
        </div>

        {/* 默认重启策略 — REQ-2.3.1 defaults.restart */}
        <div ref={(el) => { sectionRefs.current.restart = el }} style={{ scrollMarginTop: '80px' }}>
          <Card>
            <CardHeader>
              <CardTitle>默认重启策略</CardTitle>
              <CardDescription>服务的全局默认重启策略（服务未指定时使用）</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">重启策略</label>
                <Select
                  options={restartPolicyOptions}
                  value={restartDefaults.policy}
                  onChange={(e) => updateRestartDefaults({ policy: e.target.value })}
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">always=总是重启，on-failure=仅失败时重启，never=不重启</p>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">退避初始时间</label>
                  <Input
                    type="number"
                    min={0}
                    max={60000}
                    value={restartDefaults.backoff_ms}
                    onChange={(e) => updateRestartDefaults({ backoff_ms: Number(e.target.value) })}
                    placeholder="默认: 1000"
                  />
                  <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">单位：毫秒。首次重试等待时间</p>
                </div>
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">退避最大时间</label>
                  <Input
                    type="number"
                    min={100}
                    max={300000}
                    value={restartDefaults.max_backoff_ms}
                    onChange={(e) => updateRestartDefaults({ max_backoff_ms: Number(e.target.value) })}
                    placeholder="默认: 30000"
                  />
                  <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">单位：毫秒。重试等待的上限</p>
                </div>
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">退避倍率</label>
                  <Input
                    type="number"
                    min={1}
                    max={10}
                    step={0.1}
                    value={restartDefaults.multiplier}
                    onChange={(e) => updateRestartDefaults({ multiplier: Number(e.target.value) })}
                    placeholder="默认: 2"
                  />
                  <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">每次重试等待时间的增长倍数</p>
                </div>
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">最大重试次数</label>
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    value={restartDefaults.max_retries}
                    onChange={(e) => updateRestartDefaults({ max_retries: Number(e.target.value) })}
                    placeholder="默认: 0"
                  />
                  <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">0 表示无限重试</p>
                </div>
                <div className="col-span-2">
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">重置计数时间</label>
                  <Input
                    type="number"
                    min={0}
                    max={86400}
                    value={restartDefaults.reset_after_seconds}
                    onChange={(e) => updateRestartDefaults({ reset_after_seconds: Number(e.target.value) })}
                    placeholder="默认: 300"
                  />
                  <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">单位：秒。运行多久后重置重试计数</p>
                </div>
              </div>
              <Button variant="primary" size="sm" onClick={handleSave} disabled={isSaving}>
                {isSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t.settings.save}
              </Button>
            </CardContent>
          </Card>
        </div>

        {/* env_files 全局环境变量加载顺序 */}
        <div ref={(el) => { sectionRefs.current.envfiles = el }} style={{ scrollMarginTop: '80px' }}>
          <Card>
            <CardHeader>
              <CardTitle>全局环境变量文件</CardTitle>
              <CardDescription>配置 env_files 列表，决定环境变量的加载顺序</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <p className="text-xs text-[var(--color-text-tertiary)]">环境变量文件列表，按顺序加载（后加载的覆盖先加载的）。例如：env.yaml, env.local.yaml</p>
              {(formState.env_files ?? []).map((file, idx) => (
                <div key={idx} className="flex items-center gap-2">
                  <Input
                    value={file}
                    onChange={(e) => {
                      const files = [...(formState.env_files ?? [])]
                      files[idx] = e.target.value
                      setFormState({ ...formState, env_files: files })
                    }}
                    placeholder="env.yaml"
                  />
                  <Button
                    variant="danger"
                    size="sm"
                    onClick={() => {
                      const files = (formState.env_files ?? []).filter((_, i) => i !== idx)
                      setFormState({ ...formState, env_files: files })
                    }}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
              <Button
                variant="default"
                size="sm"
                onClick={() => setFormState({ ...formState, env_files: [...(formState.env_files ?? []), ''] })}
              >
                <Plus className="h-3.5 w-3.5" />
                添加文件
              </Button>
              <Button variant="primary" size="sm" onClick={handleSave} disabled={isSaving}>
                {isSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t.settings.save}
              </Button>
            </CardContent>
          </Card>
        </div>

        {/* extension_dirs 全局扩展目录 */}
        <div ref={(el) => { sectionRefs.current.extdirs = el }} style={{ scrollMarginTop: '80px' }}>
          <Card>
            <CardHeader>
              <CardTitle>扩展目录</CardTitle>
              <CardDescription>配置 extension_dirs 列表，指定扩展的加载路径</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <p className="text-xs text-[var(--color-text-tertiary)]">扩展加载路径列表，supd 会从这些目录扫描扩展。例如：/etc/supd/extensions, ./ext</p>
              {(formState.extension_dirs ?? []).map((dir, idx) => (
                <div key={idx} className="flex items-center gap-2">
                  <Input
                    value={dir}
                    onChange={(e) => {
                      const dirs = [...(formState.extension_dirs ?? [])]
                      dirs[idx] = e.target.value
                      setFormState({ ...formState, extension_dirs: dirs })
                    }}
                    placeholder="/etc/supd/extensions"
                  />
                  <Button
                    variant="danger"
                    size="sm"
                    onClick={() => {
                      const dirs = (formState.extension_dirs ?? []).filter((_, i) => i !== idx)
                      setFormState({ ...formState, extension_dirs: dirs })
                    }}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
              <Button
                variant="default"
                size="sm"
                onClick={() => setFormState({ ...formState, extension_dirs: [...(formState.extension_dirs ?? []), ''] })}
              >
                <Plus className="h-3.5 w-3.5" />
                添加目录
              </Button>
              <Button variant="primary" size="sm" onClick={handleSave} disabled={isSaving}>
                {isSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t.settings.save}
              </Button>
            </CardContent>
          </Card>
        </div>

        {/* Token 管理 */}
        <div ref={(el) => { sectionRefs.current.token = el }} style={{ scrollMarginTop: '80px' }}>
          <TokenManager tokenConfigured={data?.auth_token_configured} authMode={data?.auth_mode} />
        </div>

        {/* 全局环境变量 */}
        <div ref={(el) => { sectionRefs.current.env = el }} style={{ scrollMarginTop: '80px' }}>
          <EnvEditor />
        </div>

        {/* 运行时配置 */}
        <div ref={(el) => { sectionRefs.current.runtime = el }} style={{ scrollMarginTop: '80px' }}>
          <RuntimeSettings />
        </div>

        {/* 关于 — REQ-2.9.10 */}
        <div ref={(el) => { sectionRefs.current.about = el }} style={{ scrollMarginTop: '80px' }}>
          <Card>
            <CardHeader>
              <CardTitle>关于</CardTitle>
              <CardDescription>supd 版本与诊断信息</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <div className="flex items-start gap-3">
                <Info className="h-5 w-5 text-[var(--color-text-tertiary)] mt-0.5" />
                <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm flex-1">
                  <dt className="text-[var(--color-text-tertiary)]">supd 版本</dt>
                  <dd className="font-mono text-[var(--color-text-primary)]">{aboutInfo.version}</dd>
                  <dt className="text-[var(--color-text-tertiary)]">启动时间</dt>
                  <dd className="font-mono text-[var(--color-text-primary)]">{aboutInfo.build_time}</dd>
                  <dt className="text-[var(--color-text-tertiary)]">工作目录</dt>
                  <dd className="font-mono text-[var(--color-text-primary)]">{aboutInfo.work_dir}</dd>
                </dl>
              </div>
              <div className="flex gap-2 pt-2 border-t border-[var(--color-border-primary)]">
                <a
                  href="https://github.com/supdorg/supd/releases"
                  target="_blank"
                  rel="noreferrer"
                  className="text-sm text-[var(--color-brand-primary)] hover:underline"
                >
                  检查更新
                </a>
                <span className="text-[var(--color-text-tertiary)]">·</span>
                <button
                  onClick={async () => {
                    try {
                      const resp = await fetch('/api/system/diagnostic')
                      if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
                      const data = await resp.json()
                      // 附加浏览器环境信息
                      data.browser = {
                        userAgent: navigator.userAgent,
                        language: navigator.language,
                        platform: navigator.platform,
                      }
                      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
                      const url = URL.createObjectURL(blob)
                      const a = document.createElement('a')
                      a.href = url
                      a.download = `supd-diagnostic-${Date.now()}.json`
                      a.click()
                      URL.revokeObjectURL(url)
                      toast.success('诊断信息已导出')
                    } catch (e) {
                      toast.error('导出诊断信息失败: ' + (e as Error).message)
                    }
                  }}
                  className="text-sm text-[var(--color-brand-primary)] hover:underline"
                >
                  导出诊断信息
                </button>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}

// 运行时配置子组件
interface RuntimeEntry {
  alias: string
  path: string
  source: string
  available: boolean
}

interface RuntimesResponse {
  runtimes: RuntimeEntry[]
  default: string
}

function RuntimeSettings() {
  const queryClient = useQueryClient()
  const [newAlias, setNewAlias] = useState('')
  const [newPath, setNewPath] = useState('')
  // E-09-003: 运行时上传状态
  const [uploadAlias, setUploadAlias] = useState('')
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const uploadFileInputRef = useRef<HTMLInputElement>(null)

  const { data: runtimesData, isLoading } = useQuery({
    queryKey: ['settings-runtimes'],
    queryFn: () => apiGet<RuntimesResponse>('/api/runtimes'),
  })

  const runtimes = runtimesData?.runtimes ?? []

  const saveRuntimesMutation = useMutation({
    mutationFn: (updated: Record<string, string>) => apiPut('/api/settings/runtimes', updated, true),
    onSuccess: () => {
      toast.success('运行时配置已保存')
      queryClient.invalidateQueries({ queryKey: ['settings-runtimes'] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '保存运行时配置失败')) },
  })

  // E-09-003: 上传运行时二进制文件 — POST /api/runtimes/upload，raw body + name 查询参数
  const uploadRuntimeMutation = useMutation({
    mutationFn: async ({ alias, file }: { alias: string; file: File }) => {
      // 使用 fetch 直接调用，因为后端接收 raw body（非 JSON/multipart）
      const resp = await fetch(`/api/runtimes/upload?name=${encodeURIComponent(alias)}`, {
        method: 'POST',
        body: file,
      })
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({ error: '上传失败' }))
        throw new Error(err.error || err.message || `HTTP ${resp.status}`)
      }
      return resp.json()
    },
    onSuccess: () => {
      toast.success('运行时上传成功')
      setUploadAlias('')
      setUploadFile(null)
      if (uploadFileInputRef.current) uploadFileInputRef.current.value = ''
      queryClient.invalidateQueries({ queryKey: ['settings-runtimes'] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '上传运行时失败')) },
  })

  // E-09-003: 删除上传的运行时文件 — DELETE /api/runtimes/{name}
  const deleteRuntimeMutation = useMutation({
    mutationFn: (alias: string) => apiDelete(`/api/runtimes/${encodeURIComponent(alias)}`, true),
    onSuccess: () => {
      toast.success('运行时已删除')
      queryClient.invalidateQueries({ queryKey: ['settings-runtimes'] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '删除运行时失败')) },
  })

  const handleAdd = () => {
    if (!newAlias.trim() || !newPath.trim()) return
    const existing: Record<string, string> = {}
    for (const r of runtimes) {
      existing[r.alias] = r.path
    }
    existing[newAlias.trim()] = newPath.trim()
    saveRuntimesMutation.mutate(existing)
    setNewAlias('')
    setNewPath('')
  }

  const handleDelete = (alias: string) => {
    const updated: Record<string, string> = {}
    for (const r of runtimes) {
      if (r.alias !== alias) {
        updated[r.alias] = r.path
      }
    }
    saveRuntimesMutation.mutate(updated)
  }

  const handleUploadFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setUploadFile(file)
    // 如果别名未填写，用文件名作为默认别名
    if (!uploadAlias.trim()) {
      const baseName = file.name.replace(/\.[^.]+$/, '')
      setUploadAlias(baseName)
    }
  }

  const handleUpload = () => {
    if (!uploadAlias.trim() || !uploadFile) return
    uploadRuntimeMutation.mutate({ alias: uploadAlias.trim(), file: uploadFile })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t.settings.runtime}</CardTitle>
        <CardDescription>{t.settings.runtimeDesc}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {isLoading ? (
          <p className="text-sm text-[var(--color-text-tertiary)]">{t.common.loading}</p>
        ) : runtimes.length === 0 ? (
          <p className="text-sm text-[var(--color-text-tertiary)]">暂无运行时</p>
        ) : (
          <div className="space-y-2">
            {runtimes.map((rt) => (
              <div key={rt.alias} className="flex items-center gap-3 rounded border border-[var(--color-border-primary)] px-3 py-2">
                <span className="font-mono text-sm font-medium text-[var(--color-text-primary)] min-w-[80px]">{rt.alias}</span>
                <span className="font-mono text-sm text-[var(--color-text-secondary)] flex-1 truncate" title={rt.path}>{rt.path}</span>
                <span className="text-xs text-[var(--color-text-tertiary)]">{rt.source}</span>
                {rt.available ? (
                  <span className="text-xs text-[var(--color-text-success)]">可用</span>
                ) : (
                  <span className="text-xs text-[var(--color-text-error)]">不可用</span>
                )}
                {/* E-09-003: config 来源的可通过配置删除；其他来源用 DELETE /api/runtimes/{name} */}
                {rt.source === 'config' && (
                  <Button variant="danger" size="sm" onClick={() => handleDelete(rt.alias)} title="从配置中移除">
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
                {rt.source !== 'builtin' && rt.source !== 'config' && (
                  <Button
                    variant="danger"
                    size="sm"
                    onClick={() => deleteRuntimeMutation.mutate(rt.alias)}
                    disabled={deleteRuntimeMutation.isPending}
                    title="删除运行时文件"
                  >
                    {deleteRuntimeMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
        {/* 添加运行时（配置方式） */}
        <div className="border-t border-[var(--color-border-primary)] pt-4">
          <p className="text-sm font-medium text-[var(--color-text-primary)] mb-2">添加运行时（配置路径）</p>
          <div className="flex items-end gap-3">
            <div className="flex-1">
              <label className="text-xs text-[var(--color-text-tertiary)]">别名</label>
              <Input
                value={newAlias}
                onChange={(e) => setNewAlias(e.target.value)}
                placeholder="python3"
              />
            </div>
            <div className="flex-1">
              <label className="text-xs text-[var(--color-text-tertiary)]">路径</label>
              <Input
                value={newPath}
                onChange={(e) => setNewPath(e.target.value)}
                placeholder="/usr/bin/python3"
              />
            </div>
            <Button variant="primary" size="sm" disabled={!newAlias.trim() || !newPath.trim()} onClick={handleAdd}>
              <Plus className="h-3.5 w-3.5" />
              添加
            </Button>
          </div>
        </div>
        {/* E-09-003: 上传运行时二进制文件 */}
        <div className="border-t border-[var(--color-border-primary)] pt-4">
          <p className="text-sm font-medium text-[var(--color-text-primary)] mb-2">上传运行时文件</p>
          <p className="text-xs text-[var(--color-text-tertiary)] mb-3">
            上传自定义运行时二进制文件（如交叉编译的解释器），文件名将作为运行时别名
          </p>
          <div className="flex items-end gap-3">
            <div className="flex-1">
              <label className="text-xs text-[var(--color-text-tertiary)]">别名</label>
              <Input
                value={uploadAlias}
                onChange={(e) => setUploadAlias(e.target.value)}
                placeholder="my-python3"
              />
            </div>
            <div className="flex-1">
              <label className="text-xs text-[var(--color-text-tertiary)]">选择文件</label>
              <input
                ref={uploadFileInputRef}
                type="file"
                onChange={handleUploadFileChange}
                className="block w-full text-sm text-[var(--color-text-secondary)]
                  file:mr-3 file:py-1.5 file:px-3 file:rounded-md file:border-0
                  file:text-sm file:font-medium file:bg-[var(--color-brand-primary)]
                  file:text-white hover:file:bg-[var(--color-brand-primary-hover)]
                  cursor-pointer"
              />
            </div>
            <Button
              variant="primary"
              size="sm"
              disabled={!uploadAlias.trim() || !uploadFile || uploadRuntimeMutation.isPending}
              onClick={handleUpload}
            >
              {uploadRuntimeMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
              上传
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
