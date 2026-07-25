// REQ-U-011: 全局环境变量编辑器（弹窗模式）
// 点击按钮弹出 Dialog，内含表格化可视化编辑器 + YAML 格式参考说明
// env.yaml 格式：env: { KEY: { value: "...", enabled?: true/false, hint?: "..." } }

import { useState, useEffect, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPut } from '@/lib/api-client'
import { toast } from '@/components/ui/Toast'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '@/components/ui/Table'
import { t } from '@/lib/i18n'
import { Pencil, Plus, Trash2, ToggleLeft, ToggleRight, Save, Loader2, Eye, EyeOff, X } from 'lucide-react'
import { entriesToEnvFileJson, isSensitiveKey } from '@/lib/env-yaml'

interface EnvEntry {
  key: string
  value: string
  enabled: boolean
  hint: string
}

// 后端 /api/settings/env 返回 {env: {KEY: {value, enabled?, hint?}}}
interface EnvFileResponse {
  env: Record<string, { value: string; enabled?: boolean; hint?: string }>
}

// YAML 格式参考提示组件
function YamlFormatHint() {
  return (
    <div className="rounded-md bg-[var(--color-surface-tertiary)] p-3 text-xs text-[var(--color-text-secondary)] leading-relaxed">
      <p className="font-semibold text-[var(--color-text-primary)] mb-1">YAML 格式参考</p>
      <pre className="font-mono whitespace-pre overflow-x-auto text-[var(--color-text-secondary)]">{`env:
  VAR_NAME:
    value: "变量值"          # 必填
    enabled: false           # 可选，默认 true
    hint: "说明文字（可选）"  # 可选`}</pre>
      <p className="mt-2">
        每个变量包含 <code className="font-mono bg-[var(--color-surface-secondary)] px-1 rounded">value</code>（必填）、
        <code className="font-mono bg-[var(--color-surface-secondary)] px-1 rounded">enabled</code>（可选，默认启用）、
        <code className="font-mono bg-[var(--color-surface-secondary)] px-1 rounded">hint</code>（可选说明）三个字段。
      </p>
    </div>
  )
}

// 环境变量编辑弹窗
export function EnvEditorDialog() {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [entries, setEntries] = useState<EnvEntry[]>([])
  const [showSecrets, setShowSecrets] = useState(false)

  const { data } = useQuery({
    queryKey: ['global-env'],
    queryFn: () => apiGet<EnvFileResponse>('/api/settings/env'),
  })

  // 弹窗打开时加载最新数据
  useEffect(() => {
    if (open && data) {
      const envEntries = Object.entries(data.env ?? {}).map(([key, v]) => ({
        key,
        value: v.value ?? '',
        enabled: v.enabled !== false,
        hint: v.hint ?? '',
      }))
      setEntries(envEntries)
      setShowSecrets(false)
    }
  }, [open, data])

  const saveMutation = useMutation({
    mutationFn: (env: EnvEntry[]) => {
      return apiPut('/api/settings/env', entriesToEnvFileJson(env), true)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-env'] })
      toast.success('环境变量已保存')
      setOpen(false)
    },
    onError: () => {
      toast.error('环境变量保存失败')
    },
  })

  function addEntry() {
    setEntries([...entries, { key: '', value: '', enabled: true, hint: '' }])
  }

  function removeEntry(index: number) {
    setEntries(entries.filter((_, i) => i !== index))
  }

  function updateEntry(index: number, field: 'key' | 'value' | 'hint', val: string) {
    const updated = [...entries]
    updated[index] = { ...updated[index]!, [field]: val }
    setEntries(updated)
  }

  function toggleEnabled(index: number) {
    const updated = [...entries]
    updated[index] = { ...updated[index]!, enabled: !updated[index]!.enabled }
    setEntries(updated)
  }

  function handleSave() {
    const emptyKeys = entries.filter((e) => !e.key.trim())
    if (emptyKeys.length > 0) {
      toast.error('变量名不能为空')
      return
    }
    const keys = entries.map((e) => e.key.trim())
    const duplicates = keys.filter((k, i) => keys.indexOf(k) !== i)
    if (duplicates.length > 0) {
      toast.error(`变量名重复：${[...new Set(duplicates)].join(', ')}`)
      return
    }
    saveMutation.mutate(entries)
  }

  // ESC 关闭
  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Escape') setOpen(false)
  }, [])

  useEffect(() => {
    if (open) {
      document.addEventListener('keydown', handleKeyDown)
      return () => document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open, handleKeyDown])

  if (!open) {
    return (
      <Button variant="default" size="sm" onClick={() => setOpen(true)}>
        <Pencil className="h-3.5 w-3.5" /> 编辑环境变量
      </Button>
    )
  }

  return (
    <>
      {/* 遮罩层 */}
      <div
        className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm animate-in fade-in-0"
        onClick={() => setOpen(false)}
      />
      {/* 弹窗主体 */}
      <div
        role="dialog"
        aria-modal="true"
        className="fixed left-1/2 top-1/2 z-[51] w-full max-w-3xl -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-6 shadow-[var(--shadow-lg)] animate-in zoom-in-95 fade-in-0 focus-visible:outline-none"
      >
        {/* 关闭按钮 */}
        <button
          onClick={() => setOpen(false)}
          className="absolute right-4 top-4 rounded-sm opacity-70 transition-opacity hover:opacity-100"
        >
          <X className="h-4 w-4" />
        </button>

        {/* 标题 */}
        <div className="mb-4">
          <h2 className="text-base font-semibold text-[var(--color-text-primary)]">{t.settings.globalEnv}</h2>
          <p className="text-sm text-[var(--color-text-secondary)]">{t.settings.globalEnvDesc}</p>
        </div>

        {/* YAML 格式参考 */}
        <YamlFormatHint />

        {/* 操作栏 */}
        <div className="flex items-center justify-between mt-3 mb-2">
          <Button variant="default" size="sm" onClick={addEntry}>
            <Plus className="h-3.5 w-3.5" /> {t.settings.addEnv}
          </Button>
          <button
            onClick={() => setShowSecrets(!showSecrets)}
            className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] underline flex items-center gap-1"
          >
            {showSecrets ? <><EyeOff className="h-3 w-3" /> 隐藏敏感值</> : <><Eye className="h-3 w-3" /> 显示敏感值</>}
          </button>
        </div>

        {/* 表格 */}
        {entries.length === 0 ? (
          <div className="py-8 text-center text-sm text-[var(--color-text-tertiary)]">
            暂无环境变量，点击"添加变量"创建
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-44">Key</TableHead>
                <TableHead>Value</TableHead>
                <TableHead className="w-56">Hint (说明)</TableHead>
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
                        onChange={(e) => updateEntry(idx, 'key', e.target.value)}
                        placeholder="VAR_NAME"
                        className="h-7 text-xs font-mono"
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        type={sensitive && !showSecrets ? 'password' : 'text'}
                        value={entry.value}
                        onChange={(e) => updateEntry(idx, 'value', e.target.value)}
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
                        onChange={(e) => updateEntry(idx, 'hint', e.target.value)}
                        placeholder="说明文字（可选）"
                        className="h-7 text-xs"
                      />
                    </TableCell>
                    <TableCell>
                      <button
                        onClick={() => toggleEnabled(idx)}
                        className={`flex items-center gap-1 px-2 py-1 rounded text-xs border transition-colors ${
                          entry.enabled
                            ? 'border-[var(--color-brand-primary)] bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)]'
                            : 'border-[var(--color-border-secondary)] text-[var(--color-text-tertiary)]'
                        }`}
                        title={entry.enabled ? '已启用' : '已禁用'}
                      >
                        {entry.enabled ? <ToggleRight className="h-3.5 w-3.5" /> : <ToggleLeft className="h-3.5 w-3.5" />}
                      </button>
                    </TableCell>
                    <TableCell>
                      <Button variant="danger" size="sm" onClick={() => removeEntry(idx)}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}

        {/* 底部按钮 */}
        <div className="flex justify-end gap-2 mt-4">
          <Button variant="default" size="sm" onClick={() => setOpen(false)}>
            取消
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={handleSave}
            disabled={saveMutation.isPending}
          >
            {saveMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            {t.settings.saveEnv}
          </Button>
        </div>
      </div>
    </>
  )
}

// 卡片内嵌模式（保留用于兼容）
export function EnvEditorCard() {
  const queryClient = useQueryClient()
  const [entries, setEntries] = useState<EnvEntry[]>([])
  const [initialized, setInitialized] = useState(false)

  const { data, error } = useQuery({
    queryKey: ['global-env'],
    queryFn: () => apiGet<EnvFileResponse>('/api/settings/env'),
  })

  if (data && !initialized) {
    const envEntries = Object.entries(data.env ?? {}).map(([key, v]) => ({
      key,
      value: v.value ?? '',
      enabled: v.enabled !== false,
      hint: v.hint ?? '',
    }))
    setEntries(envEntries)
    setInitialized(true)
  }

  const saveMutation = useMutation({
    mutationFn: (env: EnvEntry[]) => {
      return apiPut('/api/settings/env', entriesToEnvFileJson(env), true)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-env'] })
      toast.success('环境变量已保存')
      setInitialized(false)
    },
    onError: () => {
      toast.error('环境变量保存失败')
    },
  })

  function addEntry() {
    setEntries([...entries, { key: '', value: '', enabled: true, hint: '' }])
  }

  function removeEntry(index: number) {
    setEntries(entries.filter((_, i) => i !== index))
  }

  function updateEntry(index: number, field: 'key' | 'value' | 'hint', val: string) {
    const updated = [...entries]
    updated[index] = { ...updated[index]!, [field]: val }
    setEntries(updated)
  }

  function toggleEnabled(index: number) {
    const updated = [...entries]
    updated[index] = { ...updated[index]!, enabled: !updated[index]!.enabled }
    setEntries(updated)
  }

  if (error) {
    return (
      <div className="text-sm text-[var(--color-text-secondary)] py-2">
        环境变量文件不存在，请先创建配置文件后重试。
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {entries.map((entry, index) => {
        const sensitive = isSensitiveKey(entry.key)
        return (
          <div key={index} className="flex items-center gap-2">
            <Input
              value={entry.key}
              onChange={(e) => updateEntry(index, 'key', e.target.value)}
              placeholder={t.settings.envKey}
              className="flex-1"
            />
            <Input
              type={sensitive ? 'password' : 'text'}
              value={entry.value}
              onChange={(e) => updateEntry(index, 'value', e.target.value)}
              placeholder={t.settings.envValue}
              className="flex-1"
            />
            <Input
              value={entry.hint}
              onChange={(e) => updateEntry(index, 'hint', e.target.value)}
              placeholder="说明（可选）"
              className="flex-1"
            />
            <button
              onClick={() => toggleEnabled(index)}
              className={`flex items-center gap-1 px-2 py-1.5 rounded text-xs border transition-colors ${
                entry.enabled
                  ? 'border-[var(--color-brand-primary)] bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)]'
                  : 'border-[var(--color-border-secondary)] text-[var(--color-text-tertiary)]'
              }`}
              title={entry.enabled ? '已启用' : '已禁用'}
            >
              {entry.enabled ? <ToggleRight className="h-3.5 w-3.5" /> : <ToggleLeft className="h-3.5 w-3.5" />}
            </button>
            <Button variant="danger" size="sm" onClick={() => removeEntry(index)}>
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        )
      })}
      <div className="flex gap-2">
        <Button variant="default" size="sm" onClick={addEntry}>
          <Plus className="h-4 w-4" />
          {t.settings.addEnv}
        </Button>
        <Button
          variant="primary"
          size="sm"
          onClick={() => saveMutation.mutate(entries)}
          disabled={saveMutation.isPending}
        >
          {saveMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {t.settings.saveEnv}
        </Button>
      </div>
    </div>
  )
}
