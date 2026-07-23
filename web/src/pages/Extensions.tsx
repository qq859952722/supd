// REQ-U-006: 扩展列表页
// 全局扩展和服务扩展分开展示，支持多选触发条件创建

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost, apiPut } from '@/lib/api-client'
import { Input } from '@/components/ui/Input'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { Select } from '@/components/ui/Select'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { SkeletonTable } from '@/components/ui/Skeleton'
import { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/Dialog'
import { t } from '@/lib/i18n'
import { toast } from '@/components/ui/Toast'
import { useTaskToast } from '@/components/ui/TaskToast'
import { Plus, Search, Play, FileText, Puzzle, Trash2, X, Loader2, Globe, Server, AlertTriangle, Upload, CheckCircle } from 'lucide-react'
import { useState, useRef } from 'react'
import { useNavigate } from 'react-router'
import { IconPicker } from '@/components/common/IconPicker'
import { getErrorMessage } from '@/lib/error-utils'

export interface ExtensionInfo {
  name: string
  version?: string
  description?: string
  display_state: string
  trigger_type: 'on_demand' | 'on_schedule' | 'service_lifecycle' | 'supd_lifecycle'
  enabled: boolean
  service?: string
  last_run_at?: string
  last_status?: string
  run_count: number
  success_count: number
  fail_count: number
  // 与后端 ExtensionSummary 顶层字段对齐（非嵌套 meta）
  triggers?: {
    on_demand?: boolean
    on_schedule?: Array<{ cron: string; action: string }>
    service_lifecycle?: Array<{ event: string; action: string }>
    supd_lifecycle?: Array<{ event: string; action: string }>
  }
  actions?: Array<{ id: string; label?: string; button_style?: string; args?: string[] }>
  concurrency?: string
  runtime?: string
  entry?: string
  config_path?: string
  env_path?: string
}

const triggerTypeLabel: Record<string, string> = {
  on_demand: t.extension.triggerOnDemand,
  on_schedule: t.extension.triggerOnSchedule,
  service_lifecycle: t.extension.triggerServiceLifecycle,
  supd_lifecycle: t.extension.triggerSupdLifecycle,
}

// 服务生命周期事件
const serviceLifecycleEvents = [
  { value: 'pre_start', label: 'pre_start — 服务启动前' },
  { value: 'post_ready', label: 'post_ready — 服务就绪后' },
  { value: 'on_failure', label: 'on_failure — 服务异常退出' },
  { value: 'pre_stop', label: 'pre_stop — 服务停止前' },
]

// supd生命周期事件
const supdLifecycleEvents = [
  { value: 'pre_start', label: 'pre_start — supd启动前' },
  { value: 'post_ready', label: 'post_ready — 服务就绪后' },
  { value: 'pre_shutdown', label: 'pre_shutdown — supd关闭前' },
]

interface ActionFormItem {
  id: string
  label: string
  button_style: 'primary' | 'default' | 'danger'
  args: string
}

interface CronScheduleItem {
  cron: string
  action: string
}

interface CreateFormState {
  name: string
  entry: string
  description: string
  version: string
  runtime: string
  identityMode: 'user' | 'uid'
  run_as: string
  run_as_uid: string
  run_as_gid: string
  run_as_groups: string
  concurrency: string
  timeout: number
  enabled: boolean
  icon: string
  service: string
  // 触发器多选
  on_demand: boolean
  on_schedule_enabled: boolean
  cron_schedules: CronScheduleItem[]
  service_lifecycle_enabled: boolean
  service_lifecycle_events: string[]
  supd_lifecycle_enabled: boolean
  supd_lifecycle_events: string[]
  actions: ActionFormItem[]
  env_text: string
}

const initialFormState: CreateFormState = {
  name: '',
  entry: '',
  description: '',
  version: '1.0.0',
  runtime: '',
  identityMode: 'user',
  run_as: '',
  run_as_uid: '',
  run_as_gid: '',
  run_as_groups: '',
  concurrency: 'replace',
  timeout: 600,
  enabled: true,
  icon: 'puzzle',
  service: '',
  on_demand: true,
  on_schedule_enabled: false,
  cron_schedules: [],
  service_lifecycle_enabled: false,
  service_lifecycle_events: [],
  supd_lifecycle_enabled: false,
  supd_lifecycle_events: [],
  actions: [],
  env_text: '',
}

// 获取扩展的所有触发器类型标签
function getTriggerBadges(ext: ExtensionInfo): string[] {
  const badges: string[] = []
  const triggers = ext.triggers
  if (triggers) {
    if (triggers.on_demand) badges.push('on_demand')
    if (triggers.on_schedule && triggers.on_schedule.length > 0) badges.push('on_schedule')
    if (triggers.service_lifecycle && triggers.service_lifecycle.length > 0) badges.push('service_lifecycle')
    if (triggers.supd_lifecycle && triggers.supd_lifecycle.length > 0) badges.push('supd_lifecycle')
  }
  if (badges.length === 0) badges.push(ext.trigger_type)
  return badges
}

export default function ExtensionsPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { runExtension: runExtensionToast } = useTaskToast()
  const [search, setSearch] = useState('')
  const [createForm, setCreateForm] = useState<CreateFormState>(initialFormState)
  const [logExt, setLogExt] = useState<string | null>(null)
  // E-09-002: 扩展导入两阶段流程状态
  const [showImportDialog, setShowImportDialog] = useState(false)
  const [importFile, setImportFile] = useState<File | null>(null)
  const [importService, setImportService] = useState('')
  const [importPreview, setImportPreview] = useState<{
    name: string
    archive_version: string
    local_version?: string
    exists_local: boolean
    service?: string
  } | null>(null)
  const importFileInputRef = useRef<HTMLInputElement>(null)

  const { data, isLoading, isError } = useQuery({
    queryKey: ['extensions', search],
    queryFn: () => apiGet<ExtensionInfo[]>('/api/extensions', { q: search }),
  })

  // 获取服务列表用于服务关联下拉
  const { data: servicesData } = useQuery({
    queryKey: ['services-for-ext'],
    queryFn: () => apiGet<{ services: { name: string }[] }>('/api/services'),
  })

  const extensions = Array.isArray(data) ? data : []
  const serviceList = servicesData?.services ?? []

  // 分离全局扩展和服务扩展
  const globalExts = extensions.filter((e) => !e.service)
  const serviceExts = extensions.filter((e) => e.service)
  // 按服务名分组
  const serviceGroups = serviceExts.reduce<Record<string, ExtensionInfo[]>>((acc, ext) => {
    const svc = ext.service!
    if (!acc[svc]) acc[svc] = []
    acc[svc].push(ext)
    return acc
  }, {})

  const createMutation = useMutation({
    mutationFn: async () => {
      // 构建 ExtensionMeta payload
      const triggers: Record<string, unknown> = {}

      if (createForm.on_demand) {
        triggers.on_demand = true
      }

      if (createForm.on_schedule_enabled) {
        triggers.on_schedule = createForm.cron_schedules
          .filter((c) => c.cron.trim())
          .map((c) => ({ cron: c.cron.trim(), action: c.action.trim() || undefined }))
      }

      if (createForm.service_lifecycle_enabled && createForm.service_lifecycle_events.length > 0) {
        // F-01-004 修复：action 字段必须引用已定义的 action，默认使用第一个 action 的 id
        const defaultActionId = createForm.actions[0]?.id || 'run'
        triggers.service_lifecycle = createForm.service_lifecycle_events.map((event) => ({
          event,
          action: defaultActionId,
        }))
      }

      if (createForm.supd_lifecycle_enabled && createForm.supd_lifecycle_events.length > 0) {
        // F-01-004 修复：action 字段必须引用已定义的 action，默认使用第一个 action 的 id
        const defaultActionId = createForm.actions[0]?.id || 'run'
        triggers.supd_lifecycle = createForm.supd_lifecycle_events.map((event) => ({
          event,
          action: defaultActionId,
        }))
      }

      const payload: Record<string, unknown> = {
        name: createForm.name,
        version: createForm.version || undefined,
        description: createForm.description || undefined,
        enabled: createForm.enabled,
        runtime: createForm.runtime || undefined,
        entry: createForm.entry,
        timeout_seconds: createForm.timeout || undefined,
        concurrency: createForm.concurrency || undefined,
        // §2.2.13: run_as（User 模式）与 run_as_uid（UID 模式）互斥，由 identityMode 决定
        ...(createForm.identityMode === 'uid'
          ? (() => {
              const groupsNums = createForm.run_as_groups.split(',').map((s) => s.trim()).filter(Boolean).map(Number).filter((n) => !isNaN(n) && n > 0)
              return {
                run_as_uid: parseInt(createForm.run_as_uid, 10) || undefined,
                run_as_gid: parseInt(createForm.run_as_gid, 10) || undefined,
                run_as_groups: groupsNums.length ? groupsNums : undefined,
              }
            })()
          : { run_as: createForm.run_as || undefined }),
        ui: { button_style: 'default', icon: createForm.icon },
        actions: createForm.actions.map((a) => ({
          id: a.id,
          // F-01-003 修复：label 必填，直接发送值（后端校验非空）
          label: a.label,
          button_style: a.button_style,
          args: a.args.trim() ? a.args.trim().split(/\s+/).filter(Boolean) : [],
        })),
        triggers,
      }
      if (createForm.service) {
        payload.service = createForm.service
      }

      // E-02 修复：silent=true 避免与 onError 重复 toast
      await apiPost('/api/extensions', payload, true)

      // 如果有 env 内容，保存环境变量
      if (createForm.env_text.trim()) {
        const env: Record<string, { value: string }> = {}
        for (const line of createForm.env_text.split('\n')) {
          const trimmed = line.trim()
          if (!trimmed || trimmed.startsWith('#')) continue
          const eqIdx = trimmed.indexOf('=')
          if (eqIdx > 0) {
            const key = trimmed.slice(0, eqIdx).trim()
            const val = trimmed.slice(eqIdx + 1).trim()
            env[key] = { value: val }
          }
        }
        if (Object.keys(env).length > 0) {
          await apiPut(`/api/extensions/${encodeURIComponent(createForm.name)}/env`, { env }, true)
        }
      }
    },
    onSuccess: () => {
      toast.success('扩展创建成功')
      queryClient.invalidateQueries({ queryKey: ['extensions'] })
      setCreateForm(initialFormState)
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '扩展创建失败')) },
  })

  // E-09-002: 扩展导入 — 两阶段流程
  // 阶段1：上传 .tar.gz 预览，获取包内扩展名和版本对比信息
  const importPreviewMutation = useMutation({
    mutationFn: async (file: File) => {
      const formData = new FormData()
      formData.append('file', file)
      // 使用 fetch 直接调用，因为 apiPost 只支持 JSON body
      const resp = await fetch('/api/extensions/import', {
        method: 'POST',
        body: formData,
      })
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({ error: '导入预览失败' }))
        throw new Error(err.error || err.message || `HTTP ${resp.status}`)
      }
      return resp.json()
    },
    onSuccess: (data) => {
      setImportPreview(data)
      toast.success(`预览完成：${data.name} v${data.archive_version}`)
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '导入预览失败')) },
  })
  // 阶段2：确认导入 — 二次上传 .tar.gz，后端备份现有目录并解压覆盖
  const importConfirmMutation = useMutation({
    mutationFn: async ({ file, name, service }: { file: File; name: string; service?: string }) => {
      const formData = new FormData()
      formData.append('file', file)
      formData.append('name', name)
      if (service) formData.append('service', service)
      const resp = await fetch('/api/extensions/import/confirm', {
        method: 'POST',
        body: formData,
      })
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({ error: '导入确认失败' }))
        throw new Error(err.error || err.message || `HTTP ${resp.status}`)
      }
      return resp.json()
    },
    onSuccess: () => {
      toast.success('扩展导入成功，已触发热重载')
      // 重置导入对话框状态
      setShowImportDialog(false)
      setImportFile(null)
      setImportPreview(null)
      setImportService('')
      if (importFileInputRef.current) importFileInputRef.current.value = ''
      queryClient.invalidateQueries({ queryKey: ['extensions'] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '导入确认失败')) },
  })
  // 处理文件选择 — 选择后立即预览
  const handleImportFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    if (!file.name.endsWith('.tar.gz') && !file.name.endsWith('.tgz')) {
      toast.error('请选择 .tar.gz 格式的扩展包')
      return
    }
    setImportFile(file)
    setImportPreview(null)
    importPreviewMutation.mutate(file)
  }
  // 确认导入
  const handleImportConfirm = () => {
    if (!importFile || !importPreview) return
    importConfirmMutation.mutate({
      file: importFile,
      name: importPreview.name,
      service: importService || undefined,
    })
  }
  // 关闭导入对话框并重置状态
  const handleImportClose = () => {
    setShowImportDialog(false)
    setImportFile(null)
    setImportPreview(null)
    setImportService('')
    if (importFileInputRef.current) importFileInputRef.current.value = ''
  }

  // 运行扩展 — 非阻塞浮动通知（不打开独占对话框）
  // 点击动作按钮后，在右下角显示浮动进度卡片
  const runExtensionWithLog = (name: string, action?: string) => {
    runExtensionToast({ extensionName: name, action })
  }

  // 表单操作辅助
  const addAction = () => {
    setCreateForm((f) => ({
      ...f,
      actions: [...f.actions, { id: '', label: '', button_style: 'default', args: '' }],
    }))
  }
  const removeAction = (idx: number) => {
    setCreateForm((f) => ({ ...f, actions: f.actions.filter((_, i) => i !== idx) }))
  }
  const updateAction = (idx: number, field: keyof ActionFormItem, value: string) => {
    setCreateForm((f) => ({
      ...f,
      actions: f.actions.map((a, i) => (i === idx ? { ...a, [field]: value } : a)),
    }))
  }

  const addCronSchedule = () => {
    setCreateForm((f) => ({
      ...f,
      cron_schedules: [...f.cron_schedules, { cron: '', action: '' }],
    }))
  }
  const removeCronSchedule = (idx: number) => {
    setCreateForm((f) => ({ ...f, cron_schedules: f.cron_schedules.filter((_, i) => i !== idx) }))
  }
  const updateCronSchedule = (idx: number, field: keyof CronScheduleItem, value: string) => {
    setCreateForm((f) => ({
      ...f,
      cron_schedules: f.cron_schedules.map((c, i) => (i === idx ? { ...c, [field]: value } : c)),
    }))
  }

  // 切换服务生命周期事件
  const toggleServiceLifecycleEvent = (event: string) => {
    setCreateForm((f) => {
      const events = f.service_lifecycle_events.includes(event)
        ? f.service_lifecycle_events.filter((e) => e !== event)
        : [...f.service_lifecycle_events, event]
      return { ...f, service_lifecycle_events: events }
    })
  }

  // 切换supd生命周期事件
  const toggleSupdLifecycleEvent = (event: string) => {
    setCreateForm((f) => {
      const events = f.supd_lifecycle_events.includes(event)
        ? f.supd_lifecycle_events.filter((e) => e !== event)
        : [...f.supd_lifecycle_events, event]
      return { ...f, supd_lifecycle_events: events }
    })
  }

  const serviceOptions = [
    { value: '', label: '全局扩展（不关联服务）' },
    ...serviceList.map((s) => ({ value: s.name, label: s.name })),
  ]

  const concurrencyOptions = [
    { value: 'replace', label: 'replace — 替换当前任务' },
    { value: 'serialize', label: 'serialize — 串行排队' },
    { value: 'parallel', label: 'parallel — 并行执行' },
    { value: 'debounce', label: 'debounce — 防抖延迟' },
  ]

  // E-09-006: 解析 concurrency 值为 { mode, seconds }
  // 规格要求 4 种并发策略，其中 debounce:Ns 需附带 N 秒防抖值
  const parsedConcurrency = (() => {
    const v = createForm.concurrency
    if (v && v.startsWith('debounce:')) {
      const match = v.match(/^debounce:(\d+)s$/)
      const seconds = match && match[1] ? parseInt(match[1], 10) : 5
      return { mode: 'debounce', seconds: isNaN(seconds) ? 5 : seconds }
    }
    if (v === 'serialize' || v === 'parallel') return { mode: v, seconds: 5 }
    return { mode: 'replace', seconds: 5 }
  })()

  // 渲染扩展表格
  const renderExtensionTable = (exts: ExtensionInfo[]) => {
    if (exts.length === 0) {
      return (
        <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
          暂无扩展
        </div>
      )
    }
    return (
      <div className="rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)]">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>描述</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>触发方式</TableHead>
              <TableHead>最后运行</TableHead>
              <TableHead className="text-right">运行次数</TableHead>
              <TableHead className="text-right">成功/失败</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {exts.map((ext) => (
              <TableRow
                key={`${ext.service || 'global'}-${ext.name}`}
                className="cursor-pointer"
                onClick={() => navigate(`/extensions/${ext.name}`)}
              >
                <TableCell>
                  <div className="flex items-center gap-2">
                    <Puzzle className="h-4 w-4 text-[var(--color-text-tertiary)] shrink-0" />
                    <div className="flex flex-col min-w-0">
                      <span className="font-medium">{ext.name}</span>
                      {ext.version && (
                        <span className="text-[10px] font-mono text-[var(--color-text-tertiary)]">v{ext.version}</span>
                      )}
                      {ext.entry && (
                        <span className="text-[10px] font-mono text-[var(--color-text-tertiary)] truncate max-w-[200px]" title={ext.entry}>
                          {ext.runtime ? `${ext.runtime} ` : ''}{ext.entry}
                        </span>
                      )}
                    </div>
                  </div>
                </TableCell>
                <TableCell className="text-sm text-[var(--color-text-secondary)] max-w-xs truncate" title={ext.description || ''}>
                  {ext.description || '-'}
                </TableCell>
                <TableCell>
                  <Badge variant={ext.enabled ? 'success' : 'secondary'}>
                    {ext.enabled ? 'enabled' : 'disabled'}
                  </Badge>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {getTriggerBadges(ext).map((tt) => (
                      <Badge key={tt} variant="default">
                        {triggerTypeLabel[tt] ?? tt}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
                <TableCell className="text-xs whitespace-nowrap">
                  {ext.last_run_at ? (
                    <div className="flex flex-col gap-0.5">
                      <span className="text-[var(--color-text-secondary)]">{ext.last_run_at ? new Date(ext.last_run_at).toLocaleString('zh-CN') : '-'}</span>
                      {ext.last_status && (
                        <Badge
                          variant={
                            ext.last_status === 'success'
                              ? 'success'
                              : ext.last_status === 'failed' || ext.last_status === 'killed'
                                ? 'danger'
                                : ext.last_status === 'timeout' || ext.last_status === 'canceled'
                                  ? 'warning'
                                  : 'default'
                          }
                        >
                          {ext.last_status}
                        </Badge>
                      )}
                    </div>
                  ) : (
                    <span className="text-[var(--color-text-tertiary)]">-</span>
                  )}
                </TableCell>
                <TableCell className="text-right font-mono text-sm">{ext.run_count}</TableCell>
                <TableCell className="text-right font-mono text-sm">
                  <span className="text-[var(--color-text-success)]">{ext.success_count}</span>
                  {' / '}
                  <span className="text-[var(--color-text-error)]">{ext.fail_count}</span>
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex items-center justify-end gap-1.5" onClick={(e) => e.stopPropagation()}>
                    {/* 有 actions 时展示每个 action 的按钮 */}
                    {ext.actions && ext.actions.length > 0 ? (
                      ext.actions.map((act) => (
                        <Button
                          key={act.id}
                          variant={act.button_style === 'primary' ? 'primary' : act.button_style === 'danger' ? 'danger' : 'default'}
                          size="sm"
                          onClick={() => runExtensionWithLog(ext.name, act.id)}
                          disabled={!ext.enabled}
                          title={`运行 action: ${act.id}`}
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
                      >
                        <Play className="h-3.5 w-3.5" />
                        {t.extension.run}
                      </Button>
                    )}
                    <Button
                      variant="default"
                      size="sm"
                      onClick={() => setLogExt(ext.name)}
                      title="查看运行历史与日志"
                    >
                      <FileText className="h-3.5 w-3.5" />
                      日志
                    </Button>
                    <Button
                      variant="default"
                      size="sm"
                      onClick={() => navigate(`/extensions/${ext.name}`)}
                    >
                      <Puzzle className="h-3.5 w-3.5" />
                      详情
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">{t.extension.title}</h1>
        <div className="flex items-center gap-2">
          {/* E-09-002: 导入扩展按钮 — 触发两阶段导入流程 */}
          <Button variant="default" size="sm" onClick={() => setShowImportDialog(true)}>
            <Upload className="h-4 w-4" />
            导入扩展
          </Button>
          <Dialog>
            <DialogTrigger>
              <Button variant="primary" size="sm">
                <Plus className="h-4 w-4" />
                {t.extension.add}
              </Button>
            </DialogTrigger>
          <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>创建扩展</DialogTitle>
            </DialogHeader>
            <div className="space-y-3">
              {/* 基本信息 */}
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">扩展名称 <span className="text-[var(--color-text-error)]">*</span></label>
                  <Input
                    value={createForm.name}
                    onChange={(e) => setCreateForm((f) => ({ ...f, name: e.target.value }))}
                    placeholder="my-extension"
                  />
                </div>
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">版本</label>
                  <Input
                    value={createForm.version}
                    onChange={(e) => setCreateForm((f) => ({ ...f, version: e.target.value }))}
                    placeholder="1.0.0"
                  />
                </div>
              </div>

              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">入口命令 <span className="text-[var(--color-text-error)]">*</span></label>
                <Input
                  value={createForm.entry}
                  onChange={(e) => setCreateForm((f) => ({ ...f, entry: e.target.value }))}
                  placeholder="/path/to/script.sh arg1 arg2"
                />
                <p className="text-xs text-[var(--color-text-tertiary)] mt-1">扩展脚本的执行路径和参数</p>
              </div>

              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">描述</label>
                <Input
                  value={createForm.description}
                  onChange={(e) => setCreateForm((f) => ({ ...f, description: e.target.value }))}
                  placeholder="扩展描述（可选）"
                />
              </div>

              <div className="grid grid-cols-3 gap-3">
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">运行时</label>
                  <Input
                    value={createForm.runtime}
                    onChange={(e) => setCreateForm((f) => ({ ...f, runtime: e.target.value }))}
                    placeholder="bash"
                  />
                  <p className="text-xs text-[var(--color-text-tertiary)] mt-1">如 bash/python3/node</p>
                </div>
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">图标</label>
                  <IconPicker
                    value={createForm.icon}
                    onChange={(v) => setCreateForm((f) => ({ ...f, icon: v }))}
                  />
                </div>
              </div>

              {/* §2.2.13: 执行身份 — User 模式与 UID 模式互斥 */}
              <div className="rounded-lg border border-[var(--color-border-secondary)] p-3">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">执行身份</label>
                  <div className="flex gap-1">
                    <button
                      type="button"
                      onClick={() => setCreateForm((f) => ({ ...f, identityMode: 'user' }))}
                      className={`px-2.5 py-1 text-xs rounded border transition-colors ${createForm.identityMode === 'user' ? 'bg-[var(--color-brand-primary)] text-white border-[var(--color-brand-primary)]' : 'border-[var(--color-border-secondary)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-secondary)]'}`}
                    >
                      按用户名（run_as）
                    </button>
                    <button
                      type="button"
                      onClick={() => setCreateForm((f) => ({ ...f, identityMode: 'uid' }))}
                      className={`px-2.5 py-1 text-xs rounded border transition-colors ${createForm.identityMode === 'uid' ? 'bg-[var(--color-brand-primary)] text-white border-[var(--color-brand-primary)]' : 'border-[var(--color-border-secondary)] text-[var(--color-text-secondary)] hover:bg-[var(--color-bg-secondary)]'}`}
                    >
                      按 UID（run_as_uid）
                    </button>
                  </div>
                </div>
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">
                  {createForm.identityMode === 'uid'
                    ? '直接指定数字 uid/gid，不依赖 /etc/passwd（适用于 NAS 固定 uid 服务）；留空则服务级扩展继承服务身份/全局扩展继承 supd 启动用户'
                    : '通过用户名查找（需存在于 /etc/passwd）；留空同上继承规则'}
                </p>
                {createForm.identityMode === 'user' ? (
                  <div className="mt-2">
                    <Input
                      value={createForm.run_as}
                      onChange={(e) => setCreateForm((f) => ({ ...f, run_as: e.target.value }))}
                      placeholder="root"
                    />
                  </div>
                ) : (
                  <div className="mt-2 grid grid-cols-3 gap-3">
                    <Input
                      type="number"
                      value={createForm.run_as_uid}
                      onChange={(e) => setCreateForm((f) => ({ ...f, run_as_uid: e.target.value }))}
                      placeholder="UID"
                    />
                    <Input
                      type="number"
                      value={createForm.run_as_gid}
                      onChange={(e) => setCreateForm((f) => ({ ...f, run_as_gid: e.target.value }))}
                      placeholder="GID（留空=UID）"
                    />
                    <Input
                      value={createForm.run_as_groups}
                      onChange={(e) => setCreateForm((f) => ({ ...f, run_as_groups: e.target.value }))}
                      placeholder="补充组（逗号分隔）"
                    />
                  </div>
                )}
              </div>

              <div className="grid grid-cols-3 gap-3">
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">并发策略</label>
                  <Select
                    options={concurrencyOptions}
                    value={parsedConcurrency.mode}
                    onChange={(e) => {
                      const mode = e.target.value
                      // E-09-006: debounce 策略需附加 Ns 防抖值，存为 debounce:Ns
                      setCreateForm((f) => ({
                        ...f,
                        concurrency: mode === 'debounce' ? `debounce:${parsedConcurrency.seconds || 5}s` : mode,
                      }))
                    }}
                  />
                  {parsedConcurrency.mode === 'debounce' && (
                    <Input
                      type="number"
                      min={1}
                      max={3600}
                      value={parsedConcurrency.seconds}
                      onChange={(e) => {
                        const n = Math.min(3600, Math.max(1, Number(e.target.value) || 5))
                        setCreateForm((f) => ({ ...f, concurrency: `debounce:${n}s` }))
                      }}
                      placeholder="5"
                      className="mt-2"
                    />
                  )}
                  {parsedConcurrency.mode === 'debounce' && (
                    <p className="text-xs text-[var(--color-text-tertiary)] mt-1">防抖秒数 N（1-3600），实际值：{createForm.concurrency}</p>
                  )}
                </div>
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">超时(秒)</label>
                  <Input
                    type="number"
                    value={createForm.timeout}
                    onChange={(e) => setCreateForm((f) => ({ ...f, timeout: Number(e.target.value) }))}
                    placeholder="600"
                  />
                  <p className="text-xs text-[var(--color-text-tertiary)] mt-1">默认600，上限1800</p>
                </div>
                <div>
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">关联服务</label>
                  <Select
                    options={serviceOptions}
                    value={createForm.service}
                    onChange={(e) => setCreateForm((f) => ({ ...f, service: e.target.value }))}
                  />
                  <p className="text-xs text-[var(--color-text-tertiary)] mt-1">选择全局或服务级</p>
                </div>
              </div>

              <div className="flex items-center gap-4">
                <label className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
                  <input
                    type="checkbox"
                    checked={createForm.enabled}
                    onChange={(e) => setCreateForm((f) => ({ ...f, enabled: e.target.checked }))}
                    className="h-4 w-4 rounded border-[var(--color-border-primary)]"
                  />
                  启用
                </label>
              </div>

              {/* 触发器配置：多选 */}
              <div className="space-y-3 rounded-md border border-[var(--color-border-secondary)] p-3 bg-[var(--color-surface-primary)]">
                <label className="text-sm font-medium text-[var(--color-text-primary)]">触发条件（可多选）</label>
                <p className="text-xs text-[var(--color-text-tertiary)]">选择扩展被触发的方式，可同时选择多种触发条件</p>

                {/* on_demand */}
                <div className="space-y-1">
                  <label className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
                    <input
                      type="checkbox"
                      checked={createForm.on_demand}
                      onChange={(e) => setCreateForm((f) => ({ ...f, on_demand: e.target.checked }))}
                      className="h-4 w-4 rounded border-[var(--color-border-primary)]"
                    />
                    <span className="font-medium">手动触发 (on_demand)</span>
                    <span className="text-xs text-[var(--color-text-tertiary)]">— 在WebUI上展示运行按钮，用户手动点击触发</span>
                  </label>
                </div>

                {/* on_schedule */}
                <div className="space-y-2">
                  <label className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
                    <input
                      type="checkbox"
                      checked={createForm.on_schedule_enabled}
                      onChange={(e) => setCreateForm((f) => ({ ...f, on_schedule_enabled: e.target.checked }))}
                      className="h-4 w-4 rounded border-[var(--color-border-primary)]"
                    />
                    <span className="font-medium">定时触发 (on_schedule)</span>
                    <span className="text-xs text-[var(--color-text-tertiary)]">— 按cron表达式定时执行</span>
                  </label>
                  {createForm.on_schedule_enabled && (
                    <div className="ml-6 space-y-2">
                      <div className="flex items-center justify-between">
                        <span className="text-xs text-[var(--color-text-tertiary)]">Cron 表达式列表</span>
                        <Button variant="default" size="sm" onClick={addCronSchedule}>
                          <Plus className="h-3.5 w-3.5" />
                          添加
                        </Button>
                      </div>
                      {createForm.cron_schedules.map((item, idx) => (
                        <div key={idx} className="flex gap-2 items-center">
                          <Input
                            value={item.cron}
                            onChange={(e) => updateCronSchedule(idx, 'cron', e.target.value)}
                            placeholder="cron 表达式，如 0 */5 * * * *"
                            className="flex-1"
                          />
                          <Input
                            value={item.action}
                            onChange={(e) => updateCronSchedule(idx, 'action', e.target.value)}
                            placeholder="action id（可选）"
                            className="flex-1"
                          />
                          <Button variant="danger" size="sm" onClick={() => removeCronSchedule(idx)}>
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                {/* service_lifecycle */}
                <div className="space-y-2">
                  <label className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
                    <input
                      type="checkbox"
                      checked={createForm.service_lifecycle_enabled}
                      onChange={(e) => setCreateForm((f) => ({ ...f, service_lifecycle_enabled: e.target.checked }))}
                      className="h-4 w-4 rounded border-[var(--color-border-primary)]"
                    />
                    <span className="font-medium">服务生命周期 (service_lifecycle)</span>
                    <span className="text-xs text-[var(--color-text-tertiary)]">— 在服务的特定生命周期阶段触发</span>
                  </label>
                  {createForm.service_lifecycle_enabled && (
                    <div className="ml-6 space-y-1">
                      <span className="text-xs text-[var(--color-text-tertiary)]">选择触发的生命周期阶段（可多选）：</span>
                      {serviceLifecycleEvents.map((evt) => (
                        <label key={evt.value} className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
                          <input
                            type="checkbox"
                            checked={createForm.service_lifecycle_events.includes(evt.value)}
                            onChange={() => toggleServiceLifecycleEvent(evt.value)}
                            className="h-4 w-4 rounded border-[var(--color-border-primary)]"
                          />
                          {evt.label}
                        </label>
                      ))}
                    </div>
                  )}
                </div>

                {/* supd_lifecycle */}
                <div className="space-y-2">
                  <label className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
                    <input
                      type="checkbox"
                      checked={createForm.supd_lifecycle_enabled}
                      onChange={(e) => setCreateForm((f) => ({ ...f, supd_lifecycle_enabled: e.target.checked }))}
                      className="h-4 w-4 rounded border-[var(--color-border-primary)]"
                    />
                    <span className="font-medium">supd生命周期 (supd_lifecycle)</span>
                    <span className="text-xs text-[var(--color-text-tertiary)]">— 在supd自身的生命周期阶段触发</span>
                  </label>
                  {createForm.supd_lifecycle_enabled && (
                    <div className="ml-6 space-y-1">
                      <span className="text-xs text-[var(--color-text-tertiary)]">选择触发的生命周期阶段（可多选）：</span>
                      {supdLifecycleEvents.map((evt) => (
                        <label key={evt.value} className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
                          <input
                            type="checkbox"
                            checked={createForm.supd_lifecycle_events.includes(evt.value)}
                            onChange={() => toggleSupdLifecycleEvent(evt.value)}
                            className="h-4 w-4 rounded border-[var(--color-border-primary)]"
                          />
                          {evt.label}
                        </label>
                      ))}
                    </div>
                  )}
                </div>
              </div>

              {/* Actions 配置 */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium text-[var(--color-text-primary)]">Actions 配置</label>
                  <Button variant="default" size="sm" onClick={addAction}>
                    <Plus className="h-3.5 w-3.5" />
                    添加 Action
                  </Button>
                </div>
                <p className="text-xs text-[var(--color-text-tertiary)]">定义扩展的可执行动作，供触发器引用</p>
                {createForm.actions.map((action, idx) => (
                  <div key={idx} className="rounded-md border border-[var(--color-border-secondary)] p-3 space-y-2 bg-[var(--color-surface-primary)]">
                    <div className="flex gap-2 items-center">
                      <Input
                        value={action.id}
                        onChange={(e) => updateAction(idx, 'id', e.target.value)}
                        placeholder="action id"
                        className="flex-1"
                      />
                      <Input
                        value={action.label}
                        onChange={(e) => updateAction(idx, 'label', e.target.value)}
                        placeholder="按钮标签（必填）"
                        className="flex-1"
                      />
                      <Select
                        className="w-28"
                        value={action.button_style}
                        onChange={(e) => updateAction(idx, 'button_style', e.target.value)}
                        options={[
                          { value: 'primary', label: 'primary' },
                          { value: 'default', label: 'default' },
                          { value: 'danger', label: 'danger' },
                        ]}
                      />
                      <Button variant="danger" size="sm" onClick={() => removeAction(idx)}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                    <Input
                      value={action.args}
                      onChange={(e) => updateAction(idx, 'args', e.target.value)}
                      placeholder="参数（空格分隔）"
                    />
                  </div>
                ))}
              </div>

              {/* 环境变量 */}
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">环境变量（每行 KEY=VALUE）</label>
                <textarea
                  value={createForm.env_text}
                  onChange={(e) => setCreateForm((f) => ({ ...f, env_text: e.target.value }))}
                  placeholder={'PATH=/usr/local/bin\nDEBUG=true'}
                  rows={3}
                  className="w-full rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] px-3 py-2 text-sm text-[var(--color-text-primary)] font-mono"
                />
              </div>
            </div>
            <DialogFooter>
              <Button
                variant="primary"
                disabled={!createForm.name || !createForm.entry}
                onClick={() => createMutation.mutate()}
              >
                创建
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        </div>
      </div>

      {/* 搜索栏 */}
      <div className="relative max-w-sm">
        <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--color-text-tertiary)]" />
        <Input
          placeholder={t.extension.searchPlaceholder}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="pl-9"
        />
      </div>

      {/* E-01-003 修复：API 错误时显示错误横幅 */}
      {isError && (
        <div className="flex items-center gap-2 rounded-md border border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-3 py-2 text-sm text-[var(--color-text-error)]">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span>扩展列表加载失败，将在稍后自动重试。</span>
        </div>
      )}

      {/* 扩展列表 — 分全局和服务 */}
      {isLoading ? (
        <SkeletonTable rows={6} cols={8} />
      ) : extensions.length > 0 ? (
        <div className="space-y-6">
          {/* 全局扩展 */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Globe className="h-4 w-4 text-[var(--color-text-tertiary)]" />
              <h2 className="text-sm font-semibold text-[var(--color-text-primary)]">全局扩展</h2>
              <Badge variant="default">{globalExts.length}</Badge>
            </div>
            {renderExtensionTable(globalExts)}
          </div>

          {/* 服务扩展 — 按服务分组 */}
          {Object.keys(serviceGroups).length > 0 && (
            <div className="space-y-4">
              <div className="flex items-center gap-2">
                <Server className="h-4 w-4 text-[var(--color-text-tertiary)]" />
                <h2 className="text-sm font-semibold text-[var(--color-text-primary)]">服务扩展</h2>
              </div>
              {Object.entries(serviceGroups).map(([serviceName, exts]) => (
                <div key={serviceName} className="space-y-2">
                  <div className="flex items-center gap-2 pl-4 border-l-2 border-[var(--color-border-secondary)]">
                    <span className="text-sm font-medium text-[var(--color-text-secondary)]">{serviceName}</span>
                    <Badge variant="default">{exts.length}</Badge>
                  </div>
                  {renderExtensionTable(exts)}
                </div>
              ))}
            </div>
          )}
        </div>
      ) : (
        <div className="flex flex-col items-center justify-center py-12 text-[var(--color-text-secondary)]">
          <p>{t.extension.empty}</p>
        </div>
      )}

      {/* 扩展运行日志对话框 — 统一左右分栏 */}
      {logExt && (
        <ExtensionLogDialog extName={logExt} onClose={() => setLogExt(null)} />
      )}

      {/* E-09-002: 扩展导入对话框 — 两阶段流程：选择文件→预览→确认 */}
      {showImportDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" onClick={handleImportClose} />
          <div className="relative z-10 w-full max-w-lg rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] shadow-md">
            {/* 头部 — 含确认按钮（预览完成后可点击） */}
            <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-5 py-3">
              <div className="flex items-center gap-2">
                <Upload className="h-4 w-4 text-[var(--color-text-tertiary)]" />
                <h3 className="text-sm font-semibold text-[var(--color-text-primary)]">导入扩展</h3>
              </div>
              <div className="flex items-center gap-1">
                {importPreview && (
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={handleImportConfirm}
                    disabled={importConfirmMutation.isPending}
                  >
                    {importConfirmMutation.isPending ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <CheckCircle className="h-3.5 w-3.5" />
                    )}
                    确认导入
                  </Button>
                )}
                <button
                  onClick={handleImportClose}
                  className="rounded-sm p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
            </div>
            {/* 内容区 */}
            <div className="p-5 space-y-4">
              {/* 文件选择 */}
              <div>
                <label className="text-sm font-medium text-[var(--color-text-primary)]">选择扩展包 (.tar.gz)</label>
                <input
                  ref={importFileInputRef}
                  type="file"
                  accept=".tar.gz,.tgz"
                  onChange={handleImportFileChange}
                  className="mt-1 block w-full text-sm text-[var(--color-text-secondary)]
                    file:mr-3 file:py-1.5 file:px-3 file:rounded-md file:border-0
                    file:text-sm file:font-medium file:bg-[var(--color-brand-primary)]
                    file:text-white hover:file:bg-[var(--color-brand-primary-hover)]
                    cursor-pointer"
                />
                <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">
                  选择扩展的 .tar.gz 压缩包，上传后将自动解析并显示版本对比信息
                </p>
              </div>
              {/* 预览中状态 */}
              {importPreviewMutation.isPending && (
                <div className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  正在解析扩展包...
                </div>
              )}
              {/* 预览结果 */}
              {importPreview && (
                <div className="rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] p-4 space-y-2">
                  <div className="flex items-center gap-2">
                    <CheckCircle className="h-4 w-4 text-[var(--color-text-success)]" />
                    <span className="text-sm font-medium text-[var(--color-text-primary)]">解析完成</span>
                  </div>
                  <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
                    <dt className="text-[var(--color-text-tertiary)]">扩展名称</dt>
                    <dd className="font-mono text-[var(--color-text-primary)]">{importPreview.name}</dd>
                    <dt className="text-[var(--color-text-tertiary)]">包内版本</dt>
                    <dd className="font-mono text-[var(--color-text-primary)]">v{importPreview.archive_version}</dd>
                    <dt className="text-[var(--color-text-tertiary)]">本地版本</dt>
                    <dd className="font-mono text-[var(--color-text-primary)]">
                      {importPreview.exists_local ? `v${importPreview.local_version}` : '不存在（新建）'}
                    </dd>
                  </dl>
                  {importPreview.exists_local && (
                    <div className="flex items-start gap-2 rounded bg-[var(--color-surface-warning)] p-2 text-xs text-[var(--color-text-warning)]">
                      <AlertTriangle className="h-3.5 w-3.5 shrink-0 mt-0.5" />
                      <span>本地已存在此扩展，确认导入将自动备份现有目录并覆盖。备份目录格式：<code className="font-mono">&lt;name&gt;.bak.&lt;timestamp&gt;</code></span>
                    </div>
                  )}
                  {/* 关联服务选择（可选） */}
                  <div>
                    <label className="text-xs font-medium text-[var(--color-text-tertiary)]">关联服务（可选）</label>
                    <Select
                      options={serviceOptions}
                      value={importService}
                      onChange={(e) => setImportService(e.target.value)}
                    />
                    <p className="mt-0.5 text-xs text-[var(--color-text-tertiary)]">选择服务后将导入为服务级扩展，不选则为全局扩展</p>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// 扩展运行日志对话框 — 统一左右分栏：左侧运行列表 + 右侧日志内容
function ExtensionLogDialog({ extName, onClose }: { extName: string; onClose: () => void }) {
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null)

  const { data: runsData, isLoading } = useQuery({
    queryKey: ['ext-runs', extName],
    queryFn: () => apiGet<Array<{
      run_id: string
      extension_name: string
      action_id: string
      state: string
      exit_code: number
      progress: number
      result_msg: string
      result_level: string
      started_at: string
      finished_at: string
      trigger_type: string
    }>>('/api/extensions/runs', { extension_name: extName, limit: 50 }),
    enabled: !!extName,
    refetchInterval: (query) => {
      const runs = query.state.data
      if (Array.isArray(runs) && runs.some((r) => r.state === 'running' || r.state === 'pending')) {
        return 2000
      }
      return false
    },
  })

  const runs = Array.isArray(runsData) ? runsData : []
  // 当前选中的 run（默认选中最新一条）
  const selectedRun = selectedRunId
    ? runs.find((r) => r.run_id === selectedRunId) ?? null
    : runs[0] ?? null
  const currentRunId = selectedRun?.run_id ?? null

  const { data: logsData, isLoading: loadingLogs } = useQuery({
    queryKey: ['ext-run-logs', currentRunId],
    queryFn: async () => {
      const resp = await apiGet<{ lines: string[]; next_pos: number; has_more: boolean }>(
        `/api/extensions/runs/${encodeURIComponent(currentRunId!)}/logs`,
        { since_pos: 0 },
      )
      return resp
    },
    enabled: !!currentRunId,
    refetchInterval: (query) => {
      if (query.state.data?.has_more) return 1000
      // 选中 run 还在运行中时持续轮询
      if (selectedRun && (selectedRun.state === 'running' || selectedRun.state === 'pending')) {
        return 1500
      }
      return false
    },
  })

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" onClick={onClose} />
      <div className="relative z-10 w-full max-w-5xl max-h-[85vh] flex flex-col rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] shadow-md">
        {/* 头部 */}
        <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-5 py-3 shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            <FileText className="h-4 w-4 text-[var(--color-text-tertiary)] shrink-0" />
            <h3 className="text-sm font-semibold text-[var(--color-text-primary)] truncate">扩展运行日志</h3>
            <Badge variant="secondary" className="font-mono text-xs shrink-0">{extName}</Badge>
          </div>
          <button onClick={onClose} className="rounded-sm p-1 text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)] shrink-0">
            <X className="h-4 w-4" />
          </button>
        </div>
        {/* 左右分栏内容 — 窄屏改纵向堆叠 */}
        <div className="flex flex-col sm:flex-row flex-1 overflow-hidden">
          {/* 左侧：运行记录列表 */}
          <div className="w-full sm:w-56 shrink-0 border-b sm:border-b-0 sm:border-r border-[var(--color-border-secondary)] overflow-auto bg-[var(--color-bg-tertiary)] max-h-[30vh] sm:max-h-none">
            {isLoading ? (
              <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                <Loader2 className="h-4 w-4 animate-spin mr-2" />{t.common.loading}
              </div>
            ) : runs.length === 0 ? (
              <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                暂无运行记录
              </div>
            ) : (
              <div className="py-1">
                {runs.map((run) => {
                  const isActive = run.run_id === currentRunId
                  return (
                    <button
                      key={run.run_id}
                      onClick={() => setSelectedRunId(run.run_id)}
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
                      <div className="text-[10px] text-[var(--color-text-tertiary)] mt-0.5 truncate" title={`${run.action_id ? run.action_id + ' · ' : ''}${run.started_at || '-'}`}>
                        {run.action_id ? `${run.action_id} · ` : ''}
                        {run.started_at || '-'}
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
            {selectedRun && (
              <div className="flex items-center gap-2 px-4 py-2 border-b border-[var(--color-border-secondary)] bg-[var(--color-surface-primary)] shrink-0">
                <Badge
                  variant={
                    selectedRun.state === 'success' ? 'success'
                      : selectedRun.state === 'failed' || selectedRun.state === 'killed' ? 'danger'
                      : selectedRun.state === 'running' || selectedRun.state === 'pending' ? 'info'
                      : 'secondary'
                  }
                >
                  {selectedRun.state === 'running' || selectedRun.state === 'pending' ? (
                    <span className="flex items-center gap-1">
                      <Loader2 className="h-3 w-3 animate-spin" />
                      {selectedRun.state} {selectedRun.progress ?? 0}%
                    </span>
                  ) : selectedRun.state}
                </Badge>
                <span className="text-xs text-[var(--color-text-tertiary)] font-mono">
                  {selectedRun.action_id || 'default'}
                </span>
                <span className="text-xs text-[var(--color-text-tertiary)]">
                  {selectedRun.started_at}
                </span>
                {selectedRun.result_msg && (
                  <span className="text-xs text-[var(--color-text-secondary)] truncate flex-1" title={selectedRun.result_msg}>
                    {selectedRun.result_msg}
                  </span>
                )}
              </div>
            )}
            {/* 日志文本 */}
            <div className="flex-1 overflow-auto p-4">
              {loadingLogs ? (
                <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                  <Loader2 className="h-4 w-4 animate-spin mr-2" />加载中...
                </div>
              ) : !currentRunId ? (
                <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                  该扩展暂无运行记录
                </div>
              ) : (logsData?.lines ?? []).length > 0 ? (
                <pre className="whitespace-pre-wrap break-all text-xs font-mono text-[var(--color-text-secondary)] leading-relaxed">
                  {(logsData?.lines ?? []).join('\n')}
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
  )
}
