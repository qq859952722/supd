// 运行时参数编辑抽屉
// 从 env.yaml 加载环境变量，展示为可编辑文本框
// 「保存」持久化到 env.yaml；「运行」仅本次生效（通过 TempEnv 传入）

import { useEffect, useMemo, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Drawer } from '@/components/ui/Drawer'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { toast } from '@/components/ui/Toast'
import { useTaskToast } from '@/components/ui/TaskToast'
import { apiGet, apiPut } from '@/lib/api-client'
import {
  parseEnvYaml,
  serializeEnvYaml,
  isSensitiveKey,
  type EnvEntry,
} from '@/lib/env-yaml'
import { getErrorMessage } from '@/lib/error-utils'
import { Save, Play, Eye, EyeOff, KeyRound } from 'lucide-react'

interface EnvParamsDrawerProps {
  open: boolean
  onOpenChange: (v: boolean) => void
  extensionName: string
  serviceName?: string
  envPath?: string
  action?: string // 指定运行的 action（空则用默认）
}

export function EnvParamsDrawer({
  open,
  onOpenChange,
  extensionName,
  serviceName,
  envPath,
  action,
}: EnvParamsDrawerProps) {
  const queryClient = useQueryClient()
  const { runExtension } = useTaskToast()
  const [entries, setEntries] = useState<EnvEntry[]>([])
  const [loaded, setLoaded] = useState(false)
  const [showSecrets, setShowSecrets] = useState(false)
  const [saving, setSaving] = useState(false)

  // 构造 env.yaml 路径
  const effectivePath = useMemo(() => {
    if (envPath) return envPath
    if (serviceName) return `services/${serviceName}/extensions/${extensionName}/env.yaml`
    return `extensions/${extensionName}/env.yaml`
  }, [envPath, serviceName, extensionName])

  // 打开时加载 env.yaml
  useEffect(() => {
    if (!open || loaded) return
    apiGet<{ content: string }>('/api/files', { path: effectivePath }, true)
      .then((res) => {
        setEntries(parseEnvYaml(res.content || 'env: {}\n'))
        setLoaded(true)
      })
      .catch(() => {
        // env.yaml 不存在时显示空列表
        setEntries([])
        setLoaded(true)
      })
  }, [open, loaded, effectivePath])

  // 关闭时重置状态
  useEffect(() => {
    if (!open) {
      setLoaded(false)
      setEntries([])
      setShowSecrets(false)
    }
  }, [open])

  const updateEntry = (idx: number, value: string) => {
    setEntries((prev) => prev.map((e, i) => (i === idx ? { ...e, value } : e)))
  }

  // 保存：序列化为 YAML 并 PUT 到 /api/files
  const handleSave = async () => {
    setSaving(true)
    try {
      const content = serializeEnvYaml(entries)
      await apiPut('/api/files?path=' + encodeURIComponent(effectivePath), { content }, true)
      toast.success('环境变量已保存到 env.yaml')
      // 刷新 env 缓存
      queryClient.invalidateQueries({ queryKey: ['extension-env', extensionName, effectivePath] })
      queryClient.invalidateQueries({ queryKey: ['service-env-for-ext', serviceName] })
    } catch (err) {
      toast.error(getErrorMessage(err, '保存环境变量失败'))
    } finally {
      setSaving(false)
    }
  }

  // 运行：构造 env map，仅本次生效（不保存）
  const handleRun = () => {
    const envMap: Record<string, string> = {}
    for (const e of entries) {
      if (e.enabled && e.key) {
        envMap[e.key] = e.value
      }
    }
    runExtension({
      extensionName,
      action,
      serviceName,
      env: envMap,
    })
    onOpenChange(false)
  }

  const hasVariables = entries.length > 0

  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      title="运行参数"
      description={hasVariables ? '修改变量值后保存或运行' : '该扩展未定义环境变量'}
      footer={
        hasVariables ? (
          <div className="flex justify-end gap-2">
            <Button variant="default" size="sm" onClick={() => setShowSecrets((v) => !v)} title={showSecrets ? '隐藏敏感值' : '显示敏感值'}>
              {showSecrets ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </Button>
            <Button variant="default" size="sm" onClick={handleSave} disabled={saving}>
              <Save className="h-4 w-4" />
              保存
            </Button>
            <Button variant="primary" size="sm" onClick={handleRun}>
              <Play className="h-4 w-4" />
              运行
            </Button>
          </div>
        ) : undefined
      }
    >
      {!loaded ? (
        <div className="py-8 text-center text-sm text-[var(--color-text-tertiary)]">加载中...</div>
      ) : !hasVariables ? (
        <div className="py-8 text-center text-sm text-[var(--color-text-tertiary)]">
          此扩展未定义环境变量。
          <br />
          可在「环境变量」标签页添加。
        </div>
      ) : (
        <div className="space-y-4">
          {entries.map((entry, idx) => {
            const sensitive = isSensitiveKey(entry.key)
            return (
              <div key={idx} className="space-y-1.5">
                <div className="flex items-center gap-1.5">
                  <label className="text-xs font-medium text-[var(--color-text-secondary)] font-mono">
                    {entry.key}
                  </label>
                  {sensitive && (
                    <KeyRound className="h-3 w-3 text-[var(--color-text-tertiary)]" />
                  )}
                  {!entry.enabled && (
                    <span className="text-xs text-[var(--color-text-tertiary)]">(已禁用)</span>
                  )}
                </div>
                {entry.hint && (
                  <p className="text-xs text-[var(--color-text-tertiary)]">{entry.hint}</p>
                )}
                <Input
                  type={sensitive && !showSecrets ? 'password' : 'text'}
                  value={entry.value}
                  onChange={(e) => updateEntry(idx, e.target.value)}
                  disabled={!entry.enabled}
                  className="h-8 text-sm font-mono"
                  placeholder={entry.hint || '请输入值'}
                />
              </div>
            )
          })}
          <div className="border-t border-[var(--color-border-secondary)] pt-3 text-xs text-[var(--color-text-tertiary)]">
            <p>「保存」将值持久化到 env.yaml</p>
            <p>「运行」仅本次生效，不修改 env.yaml</p>
          </div>
        </div>
      )}
    </Drawer>
  )
}
