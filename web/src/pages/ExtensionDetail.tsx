// REQ-U-006, REQ-F-041, REQ-F-042: 扩展详情页
// 4个标签页：概览、配置（可视化表单）、环境变量（可视化）、运行历史

import { useParams, useNavigate } from 'react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPut, apiDelete } from '@/lib/api-client'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/Tabs'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/Card'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { SkeletonCard, SkeletonTable } from '@/components/ui/Skeleton'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { toast } from '@/components/ui/Toast'
import { useTaskToast } from '@/components/ui/TaskToast'
import { MonacoEditor } from '@/components/editor/MonacoEditor'
import { t } from '@/lib/i18n'
import { getErrorMessage } from '@/lib/error-utils'
import {
  type EnvEntry,
  parseEnvYaml,
  serializeEnvYaml,
  yamlStr,
  isSensitiveKey,
} from '@/lib/env-yaml'
import { useState, useMemo, useEffect } from 'react'
import {
  Play,
  ToggleLeft,
  ToggleRight,
  Download,
  Trash2,
  CheckCircle,
  XCircle,
  AlertTriangle,
  AlertCircle,
  Ban,
  Loader2,
  Plus,
  Save,
  Code,
  FormInput,
  FlaskConical,
  Eraser,
} from 'lucide-react'

export interface TaskHistory {
  run_id: string
  state: string
  started_at: string
  finished_at?: string
  exit_code?: number
  result_msg?: string
  progress?: number
  result_level?: string
}

const statusIcon: Record<string, React.ReactNode> = {
  running: <Loader2 className="h-4 w-4 animate-spin text-[var(--color-brand-primary)]" />,
  success: <CheckCircle className="h-4 w-4 text-[var(--color-text-success)]" />,
  failed: <XCircle className="h-4 w-4 text-[var(--color-text-error)]" />,
  timeout: <AlertTriangle className="h-4 w-4 text-[var(--color-text-warning)]" />,
  // F-04-001: canceled = 用户主动取消，中性色（与 BottomDrawer/CronTasks 一致）
  canceled: <Ban className="h-4 w-4 text-[var(--color-text-tertiary)]" />,
  // F-04-002: killed = 系统强杀，警告色与 failed 区分
  killed: <AlertCircle className="h-4 w-4 text-[var(--color-accent-warning)]" />,
}

// REQ-2.9.5: Action 按钮样式映射（按 button_style 排序：primary→default→danger）
const buttonStyleOrder: Record<string, number> = { primary: 0, default: 1, danger: 2 }

interface ExtensionAction {
  id: string
  label?: string
  icon?: string
  button_style?: 'primary' | 'default' | 'danger'
  cli_args?: string[]
  enabled?: boolean
}

function OverviewTab({ ext, name }: { ext: Record<string, unknown>; name: string }) {
  const queryClient = useQueryClient()
  const { runExtension } = useTaskToast()
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)

  // E-02 修复：silent=true 避免与 onError 重复 toast，提取后端错误消息
  const toggleMutation = useMutation({
    mutationFn: () => apiPut(`/api/extensions/${encodeURIComponent(name)}`, { enabled: !(String(ext.enabled) === 'true') }, true),
    onSuccess: () => {
      toast.success('扩展状态已更新')
      queryClient.invalidateQueries({ queryKey: ['extension', name] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '更新扩展状态失败')) },
  })

  const deleteMutation = useMutation({
    mutationFn: () => apiDelete(`/api/extensions/${encodeURIComponent(name)}`, true),
    onSuccess: () => {
      toast.success('扩展已删除')
      setShowDeleteDialog(false)
      queryClient.invalidateQueries({ queryKey: ['extensions'] })
      window.history.back()
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '删除扩展失败')) },
  })

  const handleExport = async () => {
    // REQ-2.12.1: 调用后端 export 端点下载 .tar.gz
    try {
      const url = `/api/extensions/${encodeURIComponent(name)}/export`
      const response = await fetch(url)
      if (!response.ok) throw new Error(`导出失败: ${response.status}`)
      const blob = await response.blob()
      const downloadUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = downloadUrl
      a.download = `${name}.tar.gz`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(downloadUrl)
      toast.success('扩展已导出')
    } catch (err) {
      toast.error(getErrorMessage(err, '导出失败'))
    }
  }

  // 动态渲染 meta.yaml 的 actions 按钮（按 button_style 排序）
  const actions = Array.isArray(ext.actions) ? (ext.actions as ExtensionAction[]) : []
  const sortedActions = [...actions].sort((a, b) => {
    const sa = buttonStyleOrder[a.button_style ?? 'default'] ?? 1
    const sb = buttonStyleOrder[b.button_style ?? 'default'] ?? 1
    return sa - sb
  })

  return (
    <div className="space-y-4">
      {/* 状态与触发器 */}
      <Card>
        <CardHeader>
          <CardTitle>{t.extension.overview}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <span className="text-[var(--color-text-tertiary)]">{t.extension.status}</span>
              <p className="font-medium mt-0.5">{String(ext.display_state ?? '-')}</p>
            </div>
            <div>
              <span className="text-[var(--color-text-tertiary)]">{t.extension.enabled}</span>
              <p className="mt-0.5">
                <Badge variant={String(ext.enabled) === 'true' ? 'success' : 'secondary'}>
                  {String(ext.enabled) === 'true' ? t.extension.yes : t.extension.no}
                </Badge>
              </p>
            </div>
            <div>
              <span className="text-[var(--color-text-tertiary)]">{t.extension.triggerType}</span>
              <p className="font-medium mt-0.5">{String(ext.trigger_type ?? '-')}</p>
            </div>
            <div>
              <span className="text-[var(--color-text-tertiary)]">{t.extension.concurrency}</span>
              <p className="font-medium mt-0.5">{String(ext.concurrency ?? '-')}</p>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* 运行统计 */}
      <Card>
        <CardHeader>
          <CardTitle>{t.extension.stats}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-3 gap-4 text-center">
            <div>
              <div className="text-2xl font-bold text-[var(--color-text-primary)]">{String(ext.run_count ?? 0)}</div>
              <div className="text-xs text-[var(--color-text-tertiary)]">{t.extension.runCount}</div>
            </div>
            <div>
              <div className="text-2xl font-bold text-[var(--color-text-success)]">{String(ext.success_count ?? 0)}</div>
              <div className="text-xs text-[var(--color-text-tertiary)]">{t.extension.successCount}</div>
            </div>
            <div>
              <div className="text-2xl font-bold text-[var(--color-text-error)]">{String(ext.fail_count ?? 0)}</div>
              <div className="text-xs text-[var(--color-text-tertiary)]">{t.extension.failCount}</div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* 快捷操作 — Action 按钮动态渲染 */}
      <Card>
        <CardHeader>
          <CardTitle>{t.extension.actions}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap gap-2">
            {/* 动态 actions 按钮（按 button_style 排序） */}
            {sortedActions.length > 0 ? (
              sortedActions
                .filter((a) => a.enabled !== false)
                .map((action) => (
                  <Button
                    key={action.id}
                    variant={action.button_style ?? 'default'}
                    size="sm"
                    onClick={() => runExtension({ extensionName: name, action: action.id })}
                  >
                    <Play className="h-4 w-4" />
                    {action.label ?? action.id}
                  </Button>
                ))
            ) : (
              /* 没有 actions 时显示默认"运行"按钮 */
              <Button
                variant="primary"
                size="sm"
                onClick={() => runExtension({ extensionName: name })}
              >
                <Play className="h-4 w-4" />
                {t.extension.run}
              </Button>
            )}
            {/* §2.2.16: 试运行模式 — 执行扩展但不产生实际副作用（用于测试） */}
            <Button
              variant="default"
              size="sm"
              onClick={() => runExtension({ extensionName: name, dryRun: true })}
              title="试运行模式：执行但不产生实际副作用"
            >
              <FlaskConical className="h-4 w-4" />
              {t.extension.dryRun}
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={() => toggleMutation.mutate()}
              disabled={toggleMutation.isPending}
            >
              {String(ext.enabled) === 'true' ? <ToggleRight className="h-4 w-4" /> : <ToggleLeft className="h-4 w-4" />}
              {String(ext.enabled) === 'true' ? t.extension.disable : t.extension.enable}
            </Button>
            <Button variant="default" size="sm" onClick={handleExport}>
              <Download className="h-4 w-4" />
              {t.extension.export}
            </Button>
            <Button variant="danger" size="sm" onClick={() => setShowDeleteDialog(true)}>
              <Trash2 className="h-4 w-4" />
              {t.extension.delete}
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* 删除确认弹窗 — REQ-2.9.12 危险操作点击遮罩不关闭 */}
      {showDeleteDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" />
          <div className="relative z-10 w-full max-w-sm rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-6 shadow-md">
            <h3 className="text-base font-semibold text-[var(--color-text-primary)]">{t.extension.delete}</h3>
            <p className="mt-2 text-sm text-[var(--color-text-secondary)]">
              确定要删除扩展 <span className="font-semibold text-[var(--color-text-primary)]">{name}</span> 吗？此操作不可恢复。
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="default" onClick={() => setShowDeleteDialog(false)}>{t.common.cancel}</Button>
              <Button variant="danger" onClick={() => deleteMutation.mutate()} disabled={deleteMutation.isPending}>
                {t.extension.delete}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// --- 扩展配置可视化表单 ---

// 简易 YAML 值解析
function parseYamlValue(v: string): unknown {
  if (v === 'true') return true
  if (v === 'false') return false
  if (v === 'null' || v === '~') return null
  const num = Number(v)
  if (!isNaN(num) && v.trim() !== '') return num
  // 去引号
  if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) {
    return v.slice(1, -1)
  }
  return v
}

// 扩展配置表单状态
interface ExtConfigForm {
  name: string
  version: string
  description: string
  enabled: boolean
  runtime: string
  entry: string
  timeout_seconds: number
  run_as: string
  concurrency: string
  ui_show_logs: boolean
  ui_button_style: string
  actions: Array<{ id: string; label: string; button_style: string; args: string }>
  triggers_on_demand: boolean
  triggers_on_schedule: Array<{ cron: string; action: string }>
  triggers_service_lifecycle: Array<{ event: string; action: string }>
  triggers_supd_lifecycle: Array<{ event: string; action: string }>
}

// 解析 meta.yaml 文本为表单状态
function parseExtConfig(yaml: string): ExtConfigForm {
  const form: ExtConfigForm = {
    name: '', version: '', description: '', enabled: true,
    runtime: '', entry: '', timeout_seconds: 600, run_as: '', concurrency: 'replace',
    ui_show_logs: true, ui_button_style: 'default',
    actions: [],
    triggers_on_demand: false,
    triggers_on_schedule: [], triggers_service_lifecycle: [], triggers_supd_lifecycle: [],
  }

  const lines = yaml.split('\n')
  let i = 0
  let currentSection = ''

  while (i < lines.length) {
    const raw = lines[i]
    if (!raw) { i++; continue }
    const hashIdx = raw.indexOf('#')
    const line = hashIdx >= 0 ? raw.slice(0, hashIdx) : raw
    const trimmed = line.trim()
    if (!trimmed) { i++; continue }

    // 顶层字段（无缩进）
    if (!line.startsWith(' ') && !line.startsWith('\t')) {
      const colonIdx = trimmed.indexOf(':')
      if (colonIdx < 0) { i++; continue }
      const key = trimmed.slice(0, colonIdx).trim()
      const value = trimmed.slice(colonIdx + 1).trim()
      currentSection = key

      if (value) {
        switch (key) {
          case 'name': form.name = String(parseYamlValue(value)); break
          case 'version': form.version = String(parseYamlValue(value)); break
          case 'description': form.description = String(parseYamlValue(value)); break
          case 'enabled': form.enabled = parseYamlValue(value) === true; break
          case 'runtime': form.runtime = String(parseYamlValue(value)); break
          case 'entry': form.entry = String(parseYamlValue(value)); break
          case 'timeout_seconds': form.timeout_seconds = Number(parseYamlValue(value)) || 600; break
          case 'run_as': form.run_as = String(parseYamlValue(value)); break
          case 'concurrency': form.concurrency = String(parseYamlValue(value)); break
        }
        i++
      } else {
        // 块级字段，读取后续缩进行
        i++
        const blockLines: string[] = []
        while (i < lines.length) {
          const bLine = lines[i]
          if (!bLine) { i++; continue }
          if (!bLine.trim() || bLine.trim().startsWith('#')) { i++; continue }
          if (bLine.startsWith(' ') || bLine.startsWith('\t')) {
            blockLines.push(bLine)
            i++
          } else break
        }

        if (currentSection === 'ui') {
          for (const bl of blockLines) {
            const bt = bl.trim()
            if (bt.startsWith('show_logs:')) form.ui_show_logs = parseYamlValue(bt.slice(bt.indexOf(':') + 1).trim()) === true
            if (bt.startsWith('button_style:')) form.ui_button_style = String(parseYamlValue(bt.slice(bt.indexOf(':') + 1).trim()))
          }
        } else if (currentSection === 'actions') {
          // 解析 actions 列表
          let curAction: { id: string; label: string; button_style: string; args: string } | null = null
          for (const bl of blockLines) {
            const bt = bl.trim()
            if (bt.startsWith('- ')) {
              curAction = { id: '', label: '', button_style: '', args: '' }
              form.actions.push(curAction)
              const rest = bt.slice(2).trim()
              if (rest.startsWith('id:')) curAction.id = String(parseYamlValue(rest.slice(3).trim()))
            } else if (bt.includes(':') && curAction) {
              const colonIdx = bt.indexOf(':')
              const k = bt.slice(0, colonIdx).trim()
              const v = bt.slice(colonIdx + 1).trim()
              if (k === 'id') curAction.id = String(parseYamlValue(v))
              else if (k === 'label') curAction.label = String(parseYamlValue(v))
              else if (k === 'button_style') curAction.button_style = String(parseYamlValue(v))
              else if (k === 'args') curAction.args = String(parseYamlValue(v))
            }
          }
        } else if (currentSection === 'triggers') {
          // 解析 triggers
          let section = ''
          let curSched: { cron: string; action: string } | null = null
          let curSvc: { event: string; action: string } | null = null
          let curSupd: { event: string; action: string } | null = null
          for (const bl of blockLines) {
            const bt = bl.trim()
            const indent = bl.length - bt.length
            if (indent === 2 && bt.includes(':') && !bt.startsWith('-')) {
              const colonIdx = bt.indexOf(':')
              section = bt.slice(0, colonIdx).trim()
              const val = bt.slice(colonIdx + 1).trim()
              if (section === 'on_demand') form.triggers_on_demand = parseYamlValue(val) === true
            } else if (indent === 4 && bt.startsWith('- ')) {
              const rest = bt.slice(2).trim()
              if (section === 'on_schedule') {
                curSched = { cron: '', action: '' }
                form.triggers_on_schedule.push(curSched)
                if (rest.startsWith('cron:')) curSched.cron = String(parseYamlValue(rest.slice(5).trim()))
              } else if (section === 'service_lifecycle') {
                curSvc = { event: '', action: '' }
                form.triggers_service_lifecycle.push(curSvc)
                if (rest.startsWith('event:')) curSvc.event = String(parseYamlValue(rest.slice(6).trim()))
              } else if (section === 'supd_lifecycle') {
                curSupd = { event: '', action: '' }
                form.triggers_supd_lifecycle.push(curSupd)
                if (rest.startsWith('event:')) curSupd.event = String(parseYamlValue(rest.slice(6).trim()))
              }
            } else if (bt.includes(':') && indent >= 6) {
              const colonIdx = bt.indexOf(':')
              const k = bt.slice(0, colonIdx).trim()
              const v = bt.slice(colonIdx + 1).trim()
              if (section === 'on_schedule' && curSched) {
                if (k === 'cron') curSched.cron = String(parseYamlValue(v))
                else if (k === 'action') curSched.action = String(parseYamlValue(v))
              } else if (section === 'service_lifecycle' && curSvc) {
                if (k === 'event') curSvc.event = String(parseYamlValue(v))
                else if (k === 'action') curSvc.action = String(parseYamlValue(v))
              } else if (section === 'supd_lifecycle' && curSupd) {
                if (k === 'event') curSupd.event = String(parseYamlValue(v))
                else if (k === 'action') curSupd.action = String(parseYamlValue(v))
              }
            }
          }
        }
      }
    } else {
      i++
    }
  }

  return form
}

// 序列化表单状态为 meta.yaml 文本
function serializeExtConfig(form: ExtConfigForm): string {
  const lines: string[] = []
  lines.push(`name: ${yamlStr(form.name)}`)
  if (form.version) lines.push(`version: ${yamlStr(form.version)}`)
  if (form.description) lines.push(`description: ${yamlStr(form.description)}`)
  lines.push(`enabled: ${form.enabled}`)
  if (form.runtime) lines.push(`runtime: ${yamlStr(form.runtime)}`)
  lines.push(`entry: ${yamlStr(form.entry)}`)
  if (form.timeout_seconds) lines.push(`timeout_seconds: ${form.timeout_seconds}`)
  if (form.run_as) lines.push(`run_as: ${yamlStr(form.run_as)}`)
  if (form.concurrency) lines.push(`concurrency: ${yamlStr(form.concurrency)}`)

  // ui
  lines.push('ui:')
  lines.push(`  show_logs: ${form.ui_show_logs}`)
  lines.push(`  button_style: ${yamlStr(form.ui_button_style)}`)

  // actions
  if (form.actions.length > 0) {
    lines.push('actions:')
    for (const act of form.actions) {
      lines.push(`  - id: ${yamlStr(act.id)}`)
      if (act.label) lines.push(`    label: ${yamlStr(act.label)}`)
      if (act.button_style) lines.push(`    button_style: ${yamlStr(act.button_style)}`)
      if (act.args) lines.push(`    args: ${yamlStr(act.args)}`)
    }
  }

  // triggers
  lines.push('triggers:')
  lines.push(`  on_demand: ${form.triggers_on_demand}`)
  if (form.triggers_on_schedule.length > 0) {
    lines.push('  on_schedule:')
    for (const s of form.triggers_on_schedule) {
      lines.push(`    - cron: ${yamlStr(s.cron)}`)
      lines.push(`      action: ${yamlStr(s.action)}`)
    }
  }
  if (form.triggers_service_lifecycle.length > 0) {
    lines.push('  service_lifecycle:')
    for (const s of form.triggers_service_lifecycle) {
      lines.push(`    - event: ${yamlStr(s.event)}`)
      lines.push(`      action: ${yamlStr(s.action)}`)
    }
  }
  if (form.triggers_supd_lifecycle.length > 0) {
    lines.push('  supd_lifecycle:')
    for (const s of form.triggers_supd_lifecycle) {
      lines.push(`    - event: ${yamlStr(s.event)}`)
      lines.push(`      action: ${yamlStr(s.action)}`)
    }
  }

  return lines.join('\n') + '\n'
}

function ConfigTab({ name, configPath }: { name: string; configPath?: string }) {
  const queryClient = useQueryClient()
  const [yamlContent, setYamlContent] = useState('')
  const [initialized, setInitialized] = useState(false)
  const [editMode, setEditMode] = useState<'visual' | 'yaml'>('visual')

  // REQ-I-006: 通过统一文件接口读取 meta.yaml
  const { data: fileContent, isLoading } = useQuery({
    queryKey: ['extension-config', name, configPath],
    queryFn: () => apiGet<{ path: string; content: string }>('/api/files', { path: configPath }),
    enabled: !!configPath,
  })

  // 获取可用运行时列表（含内置 + 自定义）
  const { data: runtimesData } = useQuery({
    queryKey: ['runtimes'],
    queryFn: () => apiGet<{ runtimes: Array<{ alias: string; available: boolean; source: string }> }>('/api/runtimes'),
  })

  // 同步查询结果到本地state（仅初始化一次）
  if (!initialized && fileContent) {
    setYamlContent(fileContent.content)
    setInitialized(true)
  }

  // 解析 YAML 为表单状态
  const form = useMemo(() => parseExtConfig(yamlContent), [yamlContent])

  // 运行时下拉选项（动态获取 + 兜底内置 + 当前值兜底）
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
    // 如果当前值不在列表中，追加一项以保证显示
    if (form.runtime && !seen.has(form.runtime)) {
      opts.push({ value: form.runtime, label: form.runtime })
    }
    return opts
  }, [runtimesData, form.runtime])

  // 更新表单字段
  const updateForm = (patch: Partial<ExtConfigForm>) => {
    const newForm = { ...form, ...patch }
    setYamlContent(serializeExtConfig(newForm))
  }

  // REQ-2.11.4: 统一通过 PUT /api/files 保存文件
  const saveMutation = useMutation({
    mutationFn: (newContent: string) => apiPut('/api/files?path=' + encodeURIComponent(configPath!), { content: newContent }, true),
    onSuccess: () => {
      toast.success('配置已保存')
      queryClient.invalidateQueries({ queryKey: ['extension', name] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '保存配置失败')) },
  })

  if (!configPath) {
    return (
      <Card>
        <CardHeader><CardTitle>{t.extension.configEditor}</CardTitle></CardHeader>
        <CardContent>
          <div className="min-h-[200px] rounded-md border border-dashed border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] flex items-center justify-center text-sm text-[var(--color-text-tertiary)]">
            无法获取配置文件路径
          </div>
        </CardContent>
      </Card>
    )
  }

  if (isLoading) {
    return (
      <Card>
        <CardContent className="py-8 flex items-center justify-center text-sm text-[var(--color-text-tertiary)]">
          <Loader2 className="h-4 w-4 animate-spin mr-2" />
          {t.common.loading}
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="space-y-4">
      {/* 模式切换 + 保存 */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1 rounded-md border border-[var(--color-border-secondary)] p-0.5">
          <button
            onClick={() => setEditMode('visual')}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium transition-colors ${
              editMode === 'visual'
                ? 'bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)]'
                : 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'
            }`}
          >
            <FormInput className="h-3.5 w-3.5" /> 可视化
          </button>
          <button
            onClick={() => setEditMode('yaml')}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium transition-colors ${
              editMode === 'yaml'
                ? 'bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)]'
                : 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'
            }`}
          >
            <Code className="h-3.5 w-3.5" /> YAML
          </button>
        </div>
        <Button
          variant="primary"
          size="sm"
          onClick={() => saveMutation.mutate(yamlContent)}
          disabled={saveMutation.isPending}
        >
          {saveMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : <Save className="h-4 w-4 mr-1" />}
          {t.files.save}
        </Button>
      </div>

      {editMode === 'yaml' ? (
        <Card>
          <CardContent>
            <div className="h-[600px] w-full overflow-hidden rounded-md border border-[var(--color-border-secondary)]">
              <MonacoEditor
                value={yamlContent}
                onChange={setYamlContent}
                onSave={(val) => saveMutation.mutate(val)}
                filename={`${name}-meta.yaml`}
                height="600px"
              />
            </div>
          </CardContent>
        </Card>
      ) : (
        <>
          {/* 基本信息 */}
          <Card>
            <CardHeader><CardTitle>基本信息</CardTitle></CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">名称</label>
                  <Input value={form.name} onChange={(e) => updateForm({ name: e.target.value })} className="mt-1 h-8 text-sm" />
                </div>
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">版本</label>
                  <Input value={form.version} onChange={(e) => updateForm({ version: e.target.value })} className="mt-1 h-8 text-sm" />
                </div>
                <div className="col-span-2">
                  <label className="text-xs text-[var(--color-text-tertiary)]">描述</label>
                  <Input value={form.description} onChange={(e) => updateForm({ description: e.target.value })} className="mt-1 h-8 text-sm" />
                </div>
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">启用</label>
                  <div className="mt-1">
                    <button
                      onClick={() => updateForm({ enabled: !form.enabled })}
                      className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm border transition-colors ${
                        form.enabled
                          ? 'border-[var(--color-brand-primary)] bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)]'
                          : 'border-[var(--color-border-secondary)] text-[var(--color-text-tertiary)]'
                      }`}
                    >
                      {form.enabled ? <ToggleRight className="h-4 w-4" /> : <ToggleLeft className="h-4 w-4" />}
                      {form.enabled ? '已启用' : '已禁用'}
                    </button>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* 执行配置 */}
          <Card>
            <CardHeader><CardTitle>执行配置</CardTitle></CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">入口脚本</label>
                  <Input value={form.entry} onChange={(e) => updateForm({ entry: e.target.value })} className="mt-1 h-8 text-sm font-mono" placeholder="run.sh" />
                </div>
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">运行时</label>
                  <Input
                    value={form.runtime}
                    onChange={(e) => updateForm({ runtime: e.target.value })}
                    className="mt-1 h-8 text-sm font-mono"
                    placeholder="bash / sh / python3 / node 或绝对路径"
                    list="ext-config-runtime-options"
                  />
                  <datalist id="ext-config-runtime-options">
                    {runtimeOptions.filter((o) => o.value).map((o) => (
                      <option key={o.value} value={o.value} />
                    ))}
                  </datalist>
                </div>
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">超时(秒)</label>
                  <Input type="number" value={form.timeout_seconds} onChange={(e) => updateForm({ timeout_seconds: Number(e.target.value) || 600 })} className="mt-1 h-8 text-sm" />
                </div>
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">运行身份</label>
                  <Input value={form.run_as} onChange={(e) => updateForm({ run_as: e.target.value })} className="mt-1 h-8 text-sm font-mono" placeholder="root" />
                </div>
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">并发策略</label>
                  <Select
                    className="mt-1 w-full"
                    value={form.concurrency}
                    onChange={(e) => updateForm({ concurrency: e.target.value })}
                    options={[
                      { value: 'replace', label: 'replace — 替换(新任务终止旧任务)' },
                      { value: 'serialize', label: 'serialize — 串行排队' },
                      { value: 'parallel', label: 'parallel — 并行执行' },
                      { value: 'debounce:Ns', label: 'debounce:Ns — 防抖' },
                    ]}
                  />
                </div>
              </div>
            </CardContent>
          </Card>

          {/* UI 配置 */}
          <Card>
            <CardHeader><CardTitle>UI 配置</CardTitle></CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">显示日志</label>
                  <div className="mt-1">
                    <button
                      onClick={() => updateForm({ ui_show_logs: !form.ui_show_logs })}
                      className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm border transition-colors ${
                        form.ui_show_logs
                          ? 'border-[var(--color-brand-primary)] bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)]'
                          : 'border-[var(--color-border-secondary)] text-[var(--color-text-tertiary)]'
                      }`}
                    >
                      {form.ui_show_logs ? <ToggleRight className="h-4 w-4" /> : <ToggleLeft className="h-4 w-4" />}
                      {form.ui_show_logs ? '显示' : '隐藏'}
                    </button>
                  </div>
                </div>
                <div>
                  <label className="text-xs text-[var(--color-text-tertiary)]">按钮样式</label>
                  <Select
                    className="mt-1 w-full"
                    value={form.ui_button_style}
                    onChange={(e) => updateForm({ ui_button_style: e.target.value })}
                    options={[
                      { value: 'primary', label: 'primary — 主按钮' },
                      { value: 'default', label: 'default — 默认' },
                      { value: 'danger', label: 'danger — 危险' },
                    ]}
                  />
                </div>
              </div>
            </CardContent>
          </Card>

          {/* Actions 配置 */}
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>Actions (动作按钮)</CardTitle>
                <Button variant="default" size="sm" onClick={() => updateForm({ actions: [...form.actions, { id: '', label: '', button_style: 'default', args: '' }] })}>
                  <Plus className="h-3.5 w-3.5" /> 添加
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              {form.actions.length === 0 ? (
                <p className="text-center text-sm text-[var(--color-text-tertiary)] py-4">无动作按钮，点击"添加"创建</p>
              ) : (
                <div className="space-y-3">
                  {form.actions.map((act, idx) => (
                    <div key={idx} className="grid grid-cols-12 gap-2 items-center p-2 rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)]">
                      <div className="col-span-3">
                        <Input value={act.id} onChange={(e) => { const a = [...form.actions]; a[idx] = { ...act, id: e.target.value }; updateForm({ actions: a }) }} placeholder="ID" className="h-7 text-xs font-mono" />
                      </div>
                      <div className="col-span-3">
                        <Input value={act.label} onChange={(e) => { const a = [...form.actions]; a[idx] = { ...act, label: e.target.value }; updateForm({ actions: a }) }} placeholder="标签" className="h-7 text-xs" />
                      </div>
                      <div className="col-span-3">
                        <Select className="w-full" value={act.button_style} onChange={(e) => { const a = [...form.actions]; a[idx] = { ...act, button_style: e.target.value }; updateForm({ actions: a }) }} options={[
                          { value: 'primary', label: 'primary' },
                          { value: 'default', label: 'default' },
                          { value: 'danger', label: 'danger' },
                        ]} />
                      </div>
                      <div className="col-span-2">
                        <Input value={act.args} onChange={(e) => { const a = [...form.actions]; a[idx] = { ...act, args: e.target.value }; updateForm({ actions: a }) }} placeholder="参数" className="h-7 text-xs font-mono" />
                      </div>
                      <div className="col-span-1 flex justify-center">
                        <Button variant="danger" size="sm" onClick={() => updateForm({ actions: form.actions.filter((_, i) => i !== idx) })}>
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          {/* Triggers 配置 */}
          <Card>
            <CardHeader><CardTitle>Triggers (触发条件)</CardTitle></CardHeader>
            <CardContent>
              <div className="space-y-4">
                {/* on_demand */}
                <div>
                  <button
                    onClick={() => updateForm({ triggers_on_demand: !form.triggers_on_demand })}
                    className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-sm border transition-colors ${
                      form.triggers_on_demand
                        ? 'border-[var(--color-brand-primary)] bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)]'
                        : 'border-[var(--color-border-secondary)] text-[var(--color-text-tertiary)]'
                    }`}
                  >
                    {form.triggers_on_demand ? <ToggleRight className="h-4 w-4" /> : <ToggleLeft className="h-4 w-4" />}
                    on_demand — 手动触发
                  </button>
                </div>

                {/* on_schedule */}
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-medium text-[var(--color-text-secondary)]">on_schedule — 定时触发</span>
                    <Button variant="default" size="sm" onClick={() => updateForm({ triggers_on_schedule: [...form.triggers_on_schedule, { cron: '', action: '' }] })}>
                      <Plus className="h-3 w-3" /> 添加
                    </Button>
                  </div>
                  {form.triggers_on_schedule.map((s, idx) => (
                    <div key={idx} className="flex gap-2 items-center mb-2">
                      <Input value={s.cron} onChange={(e) => { const a = [...form.triggers_on_schedule]; a[idx] = { ...s, cron: e.target.value }; updateForm({ triggers_on_schedule: a }) }} placeholder="cron 表达式" className="h-7 text-xs font-mono flex-1" />
                      <Input value={s.action} onChange={(e) => { const a = [...form.triggers_on_schedule]; a[idx] = { ...s, action: e.target.value }; updateForm({ triggers_on_schedule: a }) }} placeholder="action" className="h-7 text-xs font-mono w-32" />
                      <Button variant="danger" size="sm" onClick={() => updateForm({ triggers_on_schedule: form.triggers_on_schedule.filter((_, i) => i !== idx) })}>
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    </div>
                  ))}
                </div>

                {/* service_lifecycle */}
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-medium text-[var(--color-text-secondary)]">service_lifecycle — 服务生命周期</span>
                    <Button variant="default" size="sm" onClick={() => updateForm({ triggers_service_lifecycle: [...form.triggers_service_lifecycle, { event: '', action: '' }] })}>
                      <Plus className="h-3 w-3" /> 添加
                    </Button>
                  </div>
                  {form.triggers_service_lifecycle.map((s, idx) => (
                    <div key={idx} className="flex gap-2 items-center mb-2">
                      <Select className="w-32" value={s.event} onChange={(e) => { const a = [...form.triggers_service_lifecycle]; a[idx] = { ...s, event: e.target.value }; updateForm({ triggers_service_lifecycle: a }) }} placeholder="选择事件..." options={[
                        { value: 'pre_start', label: 'pre_start' },
                        { value: 'post_ready', label: 'post_ready' },
                        { value: 'on_failure', label: 'on_failure' },
                        { value: 'pre_stop', label: 'pre_stop' },
                      ]} />
                      <Input value={s.action} onChange={(e) => { const a = [...form.triggers_service_lifecycle]; a[idx] = { ...s, action: e.target.value }; updateForm({ triggers_service_lifecycle: a }) }} placeholder="action" className="h-7 text-xs font-mono flex-1" />
                      <Button variant="danger" size="sm" onClick={() => updateForm({ triggers_service_lifecycle: form.triggers_service_lifecycle.filter((_, i) => i !== idx) })}>
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    </div>
                  ))}
                </div>

                {/* supd_lifecycle */}
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-medium text-[var(--color-text-secondary)]">supd_lifecycle — supd 生命周期</span>
                    <Button variant="default" size="sm" onClick={() => updateForm({ triggers_supd_lifecycle: [...form.triggers_supd_lifecycle, { event: '', action: '' }] })}>
                      <Plus className="h-3 w-3" /> 添加
                    </Button>
                  </div>
                  {form.triggers_supd_lifecycle.map((s, idx) => (
                    <div key={idx} className="flex gap-2 items-center mb-2">
                      <Select className="w-32" value={s.event} onChange={(e) => { const a = [...form.triggers_supd_lifecycle]; a[idx] = { ...s, event: e.target.value }; updateForm({ triggers_supd_lifecycle: a }) }} placeholder="选择事件..." options={[
                        { value: 'pre_start', label: 'pre_start' },
                        { value: 'post_ready', label: 'post_ready' },
                        { value: 'pre_shutdown', label: 'pre_shutdown' },
                      ]} />
                      <Input value={s.action} onChange={(e) => { const a = [...form.triggers_supd_lifecycle]; a[idx] = { ...s, action: e.target.value }; updateForm({ triggers_supd_lifecycle: a }) }} placeholder="action" className="h-7 text-xs font-mono flex-1" />
                      <Button variant="danger" size="sm" onClick={() => updateForm({ triggers_supd_lifecycle: form.triggers_supd_lifecycle.filter((_, i) => i !== idx) })}>
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    </div>
                  ))}
                </div>
              </div>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}

// --- 扩展环境变量可视化编辑 ---

function EnvTab({ name, envPath }: { name: string; envPath?: string }) {
  const queryClient = useQueryClient()
  const [yamlContent, setYamlContent] = useState('')
  const [initialized, setInitialized] = useState(false)
  const [editMode, setEditMode] = useState<'visual' | 'yaml'>('visual')
  const [showSecrets, setShowSecrets] = useState(false)

  // 如果没有 env_path，尝试构造路径 extensions/{name}/env.yaml
  const effectivePath = envPath || `extensions/${name}/env.yaml`

  // env.yaml 可能不存在，使用 silent 模式避免弹 toast
  const { data: fileContent, isLoading } = useQuery({
    queryKey: ['extension-env', name, effectivePath],
    queryFn: async () => {
      try {
        return await apiGet<{ path: string; content: string }>('/api/files', { path: effectivePath }, true)
      } catch {
        return { path: effectivePath, content: '' }
      }
    },
    enabled: !!name,
    retry: false,
  })

  if (!initialized && fileContent) {
    setYamlContent(fileContent.content || 'env: {}\n')
    setInitialized(true)
  }

  const entries = useMemo(() => parseEnvYaml(yamlContent), [yamlContent])

  const updateEntries = (newEntries: EnvEntry[]) => {
    setYamlContent(serializeEnvYaml(newEntries))
  }

  const saveMutation = useMutation({
    mutationFn: (newContent: string) => apiPut('/api/files?path=' + encodeURIComponent(effectivePath), { content: newContent }, true),
    onSuccess: () => {
      toast.success('环境变量已保存')
      queryClient.invalidateQueries({ queryKey: ['extension-env', name, effectivePath] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '保存环境变量失败')) },
  })

  if (isLoading) {
    return (
      <Card>
        <CardContent className="py-8 flex items-center justify-center text-sm text-[var(--color-text-tertiary)]">
          <Loader2 className="h-4 w-4 animate-spin mr-2" />
          {t.common.loading}
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="space-y-4">
      {/* 模式切换 + 保存 */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1 rounded-md border border-[var(--color-border-secondary)] p-0.5">
          <button
            onClick={() => setEditMode('visual')}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium transition-colors ${
              editMode === 'visual'
                ? 'bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)]'
                : 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'
            }`}
          >
            <FormInput className="h-3.5 w-3.5" /> 可视化
          </button>
          <button
            onClick={() => setEditMode('yaml')}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium transition-colors ${
              editMode === 'yaml'
                ? 'bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)]'
                : 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'
            }`}
          >
            <Code className="h-3.5 w-3.5" /> YAML
          </button>
        </div>
        <Button
          variant="primary"
          size="sm"
          onClick={() => saveMutation.mutate(yamlContent)}
          disabled={saveMutation.isPending}
        >
          {saveMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : <Save className="h-4 w-4 mr-1" />}
          {t.files.save}
        </Button>
      </div>

      {editMode === 'yaml' ? (
        <Card>
          <CardContent>
            <div className="h-[500px] w-full overflow-hidden rounded-md border border-[var(--color-border-secondary)]">
              <MonacoEditor
                value={yamlContent}
                onChange={setYamlContent}
                onSave={(val) => saveMutation.mutate(val)}
                filename={`${name}-env.yaml`}
                height="500px"
              />
            </div>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <div>
                <CardTitle>{t.extension.envEditor}</CardTitle>
                <CardDescription>{t.extension.envDesc}</CardDescription>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setShowSecrets(!showSecrets)}
                  className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] underline"
                >
                  {showSecrets ? '隐藏敏感值' : '显示敏感值'}
                </button>
                <Button
                  variant="default"
                  size="sm"
                  onClick={() => updateEntries([...entries, { key: '', value: '', enabled: true, hint: '' }])}
                >
                  <Plus className="h-3.5 w-3.5" /> 添加变量
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            {entries.length === 0 ? (
              <div className="py-8 text-center text-sm text-[var(--color-text-tertiary)]">
                暂无环境变量，点击"添加变量"创建
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-48">Key</TableHead>
                    <TableHead>Value</TableHead>
                    <TableHead className="w-64">Hint (说明)</TableHead>
                    <TableHead className="w-20">启用</TableHead>
                    <TableHead className="w-12">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {entries.map((entry, idx) => {
                    const sensitive = isSensitiveKey(entry.key)
                    return (
                      <TableRow key={idx}>
                        <TableCell>
                          <Input
                            value={entry.key}
                            onChange={(e) => {
                              const a = [...entries]; a[idx] = { ...entry, key: e.target.value }
                              updateEntries(a)
                            }}
                            placeholder="VAR_NAME"
                            className="h-7 text-xs font-mono"
                          />
                        </TableCell>
                        <TableCell>
                          <Input
                            type={sensitive && !showSecrets ? 'password' : 'text'}
                            value={entry.value}
                            onChange={(e) => {
                              const a = [...entries]; a[idx] = { ...entry, value: e.target.value }
                              updateEntries(a)
                            }}
                            placeholder={sensitive ? '••••••' : 'value'}
                            className="h-7 text-xs font-mono"
                          />
                          {sensitive && (
                            <span className="text-[10px] text-[var(--color-text-tertiary)]">敏感字段</span>
                          )}
                        </TableCell>
                        <TableCell>
                          <Input
                            value={entry.hint}
                            onChange={(e) => {
                              const a = [...entries]; a[idx] = { ...entry, hint: e.target.value }
                              updateEntries(a)
                            }}
                            placeholder="说明文字（可选）"
                            className="h-7 text-xs"
                          />
                        </TableCell>
                        <TableCell>
                          <button
                            onClick={() => {
                              const a = [...entries]; a[idx] = { ...entry, enabled: !entry.enabled }
                              updateEntries(a)
                            }}
                            className={`flex items-center gap-1 px-2 py-1 rounded text-xs border transition-colors ${
                              entry.enabled
                                ? 'border-[var(--color-brand-primary)] bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)]'
                                : 'border-[var(--color-border-secondary)] text-[var(--color-text-tertiary)]'
                            }`}
                          >
                            {entry.enabled ? <ToggleRight className="h-3.5 w-3.5" /> : <ToggleLeft className="h-3.5 w-3.5" />}
                            {entry.enabled ? '是' : '否'}
                          </button>
                        </TableCell>
                        <TableCell>
                          <Button
                            variant="danger"
                            size="sm"
                            onClick={() => updateEntries(entries.filter((_, i) => i !== idx))}
                          >
                            <Trash2 className="h-3 w-3" />
                          </Button>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}

// 运行历史 + 日志统一分栏视图（左：运行记录列表；右：日志内容）
// 支持 refetchInterval 实时轮询运行中任务的日志进度
function HistoryTab({ tasks }: { tasks: TaskHistory[] }) {
  const queryClient = useQueryClient()
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null)

  // 默认选中最新一条（useEffect 避免渲染中 setState）
  useEffect(() => {
    if (!selectedRunId && tasks.length > 0 && tasks[0]) {
      setSelectedRunId(tasks[0].run_id)
    }
  }, [tasks, selectedRunId])

  const selectedTask = tasks.find((t) => t.run_id === selectedRunId)
  const isRunning = selectedTask?.state === 'running' || selectedTask?.state === 'pending'

  // 日志查询 — 运行中时每 1.5s 轮询
  const { data: logsData, isLoading: loadingLogs } = useQuery({
    queryKey: ['run-logs', selectedRunId],
    queryFn: async () => {
      const resp = await apiGet<{ lines: string[]; next_pos: number; has_more: boolean }>(
        `/api/extensions/runs/${encodeURIComponent(selectedRunId!)}/logs`,
        { since_pos: 0, wait: 'true' },
      )
      return resp
    },
    enabled: !!selectedRunId,
    refetchInterval: isRunning ? 1500 : false,
  })

  if (tasks.length === 0) {
    return (
      <Card>
        <CardContent className="py-8 text-center text-sm text-[var(--color-text-secondary)]">
          {t.extension.noHistory}
        </CardContent>
      </Card>
    )
  }

  const lines = logsData?.lines ?? []

  return (
    <Card>
      <CardContent className="p-0">
        <div className="flex h-[600px]">
          {/* 左侧：运行记录列表 */}
          <div className="w-72 border-r border-[var(--color-border-primary)] overflow-y-auto">
            <div className="sticky top-0 bg-[var(--color-surface-secondary)] border-b border-[var(--color-border-primary)] px-3 py-2 text-xs font-medium text-[var(--color-text-tertiary)]">
              运行记录 ({tasks.length})
            </div>
            {tasks.slice(0, 50).map((task) => {
              const isSelected = task.run_id === selectedRunId
              const taskRunning = task.state === 'running' || task.state === 'pending'
              return (
                <button
                  key={task.run_id}
                  onClick={() => setSelectedRunId(task.run_id)}
                  className={`w-full text-left px-3 py-2 border-b border-[var(--color-border-secondary)] transition-colors ${
                    isSelected
                      ? 'bg-[var(--color-surface-hover)] border-l-2 border-l-[var(--color-brand-primary)]'
                      : 'hover:bg-[var(--color-surface-hover)]'
                  }`}
                >
                  <div className="flex items-center gap-2 mb-1">
                    {statusIcon[task.state]}
                    <span className="text-xs font-mono text-[var(--color-text-secondary)] truncate">
                      {task.run_id.slice(0, 8)}
                    </span>
                    {taskRunning && (
                      <Badge variant="info" className="text-[10px] px-1 py-0">运行中</Badge>
                    )}
                  </div>
                  <div className="text-[10px] text-[var(--color-text-tertiary)] mb-0.5">
                    {task.started_at ? new Date(task.started_at).toLocaleString('zh-CN') : '-'}
                  </div>
                  {taskRunning && typeof task.progress === 'number' && task.progress > 0 && (
                    <div className="mb-0.5">
                      <div className="flex items-center justify-between text-[10px] text-[var(--color-text-tertiary)] mb-0.5">
                        <span>进度</span>
                        <span className="font-mono">{task.progress}%</span>
                      </div>
                      <div className="h-0.5 w-full rounded-full bg-[var(--color-surface-tertiary)] overflow-hidden">
                        <div
                          className="h-full rounded-full bg-[var(--color-brand-primary)] transition-all duration-300"
                          style={{ width: `${Math.min(task.progress, 100)}%` }}
                        />
                      </div>
                    </div>
                  )}
                  {task.result_msg && (
                    <div className="text-[11px] text-[var(--color-text-secondary)] truncate">
                      {task.result_msg}
                    </div>
                  )}
                </button>
              )
            })}
          </div>

          {/* 右侧：日志内容 */}
          <div className="flex-1 flex flex-col">
            {/* 状态栏 */}
            <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-4 py-2 bg-[var(--color-surface-secondary)]">
              <div className="flex items-center gap-2">
                {selectedTask && statusIcon[selectedTask.state]}
                <span className="text-xs font-mono text-[var(--color-text-secondary)]">
                  {selectedRunId?.slice(0, 8)}
                </span>
                {selectedTask && (
                  <Badge
                    variant={selectedTask.state === 'success' ? 'success' : selectedTask.state === 'running' ? 'info' : (selectedTask.state === 'failed' || selectedTask.state === 'killed') ? 'danger' : 'warning'}
                    className="text-[10px]"
                  >
                    {selectedTask.state}
                  </Badge>
                )}
                {isRunning && (
                  <span className="text-[10px] text-[var(--color-brand-primary)] flex items-center gap-1">
                    <Loader2 className="h-3 w-3 animate-spin" />
                    实时更新中
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                {selectedTask?.exit_code != null && (
                  <span className="text-[10px] text-[var(--color-text-tertiary)]">
                    退出码: {selectedTask.exit_code}
                  </span>
                )}
                {selectedRunId && (
                  <button
                    onClick={() => {
                      apiDelete(`/api/extensions/runs/${encodeURIComponent(selectedRunId)}/logs`).then(() => {
                        queryClient.invalidateQueries({ queryKey: ['run-logs', selectedRunId] })
                      })
                    }}
                    className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)] rounded px-2 py-0.5 flex items-center gap-1"
                    title="清空当前运行的日志"
                  >
                    <Eraser className="h-3 w-3" /> 清空
                  </button>
                )}
              </div>
            </div>
            {/* 日志内容 */}
            <div className="flex-1 overflow-auto p-3">
              {loadingLogs ? (
                <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                  <Loader2 className="h-4 w-4 animate-spin mr-2" />
                  {t.common.loading}
                </div>
              ) : lines.length > 0 ? (
                <pre className="whitespace-pre-wrap break-all text-xs font-mono text-[var(--color-text-secondary)] leading-relaxed">
                  {lines.join('\n')}
                </pre>
              ) : (
                <div className="flex items-center justify-center py-8 text-sm text-[var(--color-text-tertiary)]">
                  暂无日志
                </div>
              )}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

export default function ExtensionDetailPage() {
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()

  const { data: ext, isLoading: loadingExt, isError: errorExt } = useQuery({
    queryKey: ['extension', name],
    queryFn: () => apiGet<Record<string, unknown>>(`/api/extensions/${name}`),
    enabled: !!name,
  })

  const { data: historyData, isLoading: loadingHistory } = useQuery({
    queryKey: ['extension-runs', name],
    queryFn: () => apiGet<TaskHistory[]>(`/api/extensions/runs`, { extension: name }),
    enabled: !!name,
    // 有运行中任务时每 2s 轮询，更新列表状态
    refetchInterval: (query) => {
      const runs = query.state.data as TaskHistory[] | undefined
      const hasRunning = runs?.some((r) => r.state === 'running' || r.state === 'pending')
      return hasRunning ? 2000 : false
    },
  })

  if (!name) return null

  // E-01-002: 加载时显示 skeleton 占位，避免白屏
  if (loadingExt) {
    return (
      <div className="space-y-4">
        <SkeletonCard />
        <SkeletonCard />
      </div>
    )
  }

  // E-01-004 修复：加载失败时显示明确提示，与 ServiceDetail 一致
  if (errorExt || !ext) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-20">
        <AlertTriangle className="h-10 w-10 text-[var(--color-text-warning)]" />
        <p className="text-sm text-[var(--color-text-secondary)]">
          扩展「{name}」不存在或加载失败
        </p>
        <Button variant="default" size="sm" onClick={() => navigate('/extensions')}>
          返回扩展列表
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* 面包屑 + 标题 */}
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-[var(--color-text-primary)]">{name}</h1>
        <Badge variant={String(ext?.display_state) === 'active' ? 'success' : String(ext?.display_state) === 'failed' ? 'danger' : 'default'}>
          {String(ext?.display_state ?? '-')}
        </Badge>
      </div>

      {/* 4标签页 */}
      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">{t.extension.tabOverview}</TabsTrigger>
          <TabsTrigger value="config">{t.extension.tabConfig}</TabsTrigger>
          <TabsTrigger value="env">{t.extension.tabEnv}</TabsTrigger>
          <TabsTrigger value="history">{t.extension.tabHistory}</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          {ext && <OverviewTab ext={ext} name={name} />}
        </TabsContent>

        <TabsContent value="config">
          <ConfigTab name={name!} configPath={ext?.config_path ? String(ext.config_path) : undefined} />
        </TabsContent>

        <TabsContent value="env">
          <EnvTab name={name!} envPath={ext?.env_path ? String(ext.env_path) : undefined} />
        </TabsContent>

        <TabsContent value="history">
          {loadingHistory ? <SkeletonTable rows={5} cols={5} /> : <HistoryTab tasks={Array.isArray(historyData) ? historyData : []} />}
        </TabsContent>
      </Tabs>
    </div>
  )
}
