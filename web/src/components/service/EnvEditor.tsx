// REQ-U-005: 环境变量编辑器
// 本服务env(可编辑)+继承env(折叠只读)+合并env(只读)+覆盖关系高亮
import { useState } from 'react'
import { t } from '@/lib/i18n'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/Table'
import { ChevronDown, ChevronRight, Plus, Trash2, Save, ToggleLeft, ToggleRight } from 'lucide-react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiPut } from '@/lib/api-client'
import { toast } from '@/components/ui/Toast'
import { getErrorMessage } from '@/lib/error-utils'
import { entriesToEnvFileJson, isSensitiveKey } from '@/lib/env-yaml'

interface EnvEntry {
  key: string
  value: string
  source: 'service' | 'inherited'
  overridden?: boolean
  enabled?: boolean  // 仅 service section（可编辑）
  hint?: string      // 仅 service section（可编辑）
}

interface EnvEditorProps {
  serviceName: string
  serviceEnv: EnvEntry[]
  inheritedEnv: EnvEntry[]
  mergedEnv: EnvEntry[]
}

function EnvSection({
  title,
  entries,
  editable,
  serviceName,
  isCollapsible,
}: {
  title: string
  entries: EnvEntry[]
  editable: boolean
  serviceName: string
  isCollapsible?: boolean
}) {
  const [collapsed, setCollapsed] = useState(false)
  const [editEntries, setEditEntries] = useState(entries)
  const [newKey, setNewKey] = useState('')
  const [newValue, setNewValue] = useState('')
  const queryClient = useQueryClient()

  // E-02 修复：silent=true 避免与 onError 重复 toast，提取后端错误消息
  // 发送 config.EnvFile JSON 格式（{env:{KEY:{value,enabled?,hint?}}}），与后端 handleSaveServiceEnv 一致
  const saveMutation = useMutation({
    mutationFn: (envs: EnvEntry[]) => {
      // 本地 EnvEntry（enabled?/hint? 可选）→ lib EnvEntry（enabled/hint 必填）
      const libEntries = envs.map((e) => ({
        key: e.key,
        value: e.value,
        enabled: e.enabled !== false,
        hint: e.hint ?? '',
      }))
      return apiPut(`/api/services/${encodeURIComponent(serviceName)}/env`, entriesToEnvFileJson(libEntries), true)
    },
    onSuccess: () => {
      toast.success('环境变量已保存')
      // 与 ServiceDetail 读取 env.yaml 的 queryKey 一致，保存后刷新文件内容
      queryClient.invalidateQueries({ queryKey: ['service-env-file', serviceName] })
    },
    onError: (err: unknown) => {
      toast.error(getErrorMessage(err, '保存环境变量失败'))
    },
  })

  const handleAdd = () => {
    if (!newKey.trim()) return
    setEditEntries([...editEntries, { key: newKey.trim(), value: newValue, source: 'service', enabled: true, hint: '' }])
    setNewKey('')
    setNewValue('')
  }

  const handleRemove = (key: string) => {
    setEditEntries(editEntries.filter((e) => e.key !== key))
  }

  const handleChange = (key: string, value: string) => {
    setEditEntries(editEntries.map((e) => (e.key === key ? { ...e, value } : e)))
  }

  const handleChangeHint = (key: string, hint: string) => {
    setEditEntries(editEntries.map((e) => (e.key === key ? { ...e, hint } : e)))
  }

  const handleToggleEnabled = (key: string) => {
    setEditEntries(editEntries.map((e) => (e.key === key ? { ...e, enabled: !e.enabled } : e)))
  }

  const handleSave = () => {
    saveMutation.mutate(editEntries)
  }

  return (
    <div>
      <div
        className="flex items-center gap-2 mb-3 cursor-pointer"
        onClick={() => isCollapsible && setCollapsed(!collapsed)}
      >
        {isCollapsible && (
          collapsed ? <ChevronRight className="h-4 w-4 text-[var(--color-text-tertiary)]" /> : <ChevronDown className="h-4 w-4 text-[var(--color-text-tertiary)]" />
        )}
        <h4 className="text-sm font-medium text-[var(--color-text-primary)]">{title}</h4>
        <Badge variant="secondary">{entries.length}</Badge>
        {editable && <Badge variant="info">{t.service.editable}</Badge>}
        {!editable && <Badge variant="secondary">{t.service.readOnly}</Badge>}
      </div>

      {!collapsed && (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Key</TableHead>
                <TableHead>Value</TableHead>
                {editable && <TableHead>说明</TableHead>}
                {editable && <TableHead className="w-20">启用</TableHead>}
                <TableHead>来源</TableHead>
                {editable && <TableHead>操作</TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {(editable ? editEntries : entries).map((entry) => {
                const sensitive = isSensitiveKey(entry.key)
                return (
                  <TableRow key={entry.key} className={entry.overridden ? 'bg-[var(--color-surface-warning)]' : ''}>
                    <TableCell className="font-mono text-sm">
                      {entry.key}
                      {entry.overridden && (
                        <Badge variant="warning" className="ml-2 text-[10px]">覆盖</Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      {editable ? (
                        <div>
                          <Input
                            type={sensitive ? 'password' : 'text'}
                            value={entry.value}
                            onChange={(e) => handleChange(entry.key, e.target.value)}
                            className="font-mono text-sm h-7"
                            placeholder={sensitive ? '••••••' : 'value'}
                          />
                          {sensitive && (
                            <span className="text-[10px] text-[var(--color-text-tertiary)]">敏感字段</span>
                          )}
                        </div>
                      ) : (
                        <span className="font-mono text-sm text-[var(--color-text-secondary)]">{entry.value}</span>
                      )}
                    </TableCell>
                    {editable && (
                      <TableCell>
                        <Input
                          value={entry.hint ?? ''}
                          onChange={(e) => handleChangeHint(entry.key, e.target.value)}
                          placeholder="说明（可选）"
                          className="text-sm h-7"
                        />
                      </TableCell>
                    )}
                    {editable && (
                      <TableCell>
                        <button
                          onClick={() => handleToggleEnabled(entry.key)}
                          className={`flex items-center gap-1 px-2 py-1 rounded text-xs border transition-colors ${
                            entry.enabled !== false
                              ? 'border-[var(--color-brand-primary)] bg-[var(--color-brand-primary)]/10 text-[var(--color-brand-primary)]'
                              : 'border-[var(--color-border-secondary)] text-[var(--color-text-tertiary)]'
                          }`}
                          title={entry.enabled !== false ? '已启用' : '已禁用'}
                        >
                          {entry.enabled !== false ? <ToggleRight className="h-3.5 w-3.5" /> : <ToggleLeft className="h-3.5 w-3.5" />}
                        </button>
                      </TableCell>
                    )}
                    <TableCell>
                      <Badge variant={entry.source === 'service' ? 'info' : 'secondary'}>
                        {entry.source === 'service' ? '本服务' : '继承'}
                      </Badge>
                    </TableCell>
                    {editable && (
                      <TableCell>
                        <Button variant="danger" size="sm" onClick={() => handleRemove(entry.key)}>
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </TableCell>
                    )}
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>

          {editable && (
            <div className="mt-3 space-y-2">
              <div className="flex gap-2">
                <Input
                  placeholder="KEY"
                  value={newKey}
                  onChange={(e) => setNewKey(e.target.value)}
                  className="font-mono text-sm h-8 flex-1"
                />
                <Input
                  placeholder="VALUE"
                  value={newValue}
                  onChange={(e) => setNewValue(e.target.value)}
                  className="font-mono text-sm h-8 flex-1"
                />
                <Button variant="default" size="sm" onClick={handleAdd}>
                  <Plus className="h-3.5 w-3.5" />
                  添加
                </Button>
              </div>
              <div className="flex justify-end">
                <Button
                  variant="primary"
                  size="sm"
                  onClick={handleSave}
                  disabled={saveMutation.isPending}
                >
                  <Save className="h-3.5 w-3.5" />
                  {t.service.save}
                </Button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

export function EnvEditor({ serviceName, serviceEnv, inheritedEnv, mergedEnv }: EnvEditorProps) {
  return (
    <div className="space-y-6">
      <EnvSection
        title={t.service.serviceEnv}
        entries={serviceEnv}
        editable
        serviceName={serviceName}
      />
      <EnvSection
        title={t.service.inheritedEnv}
        entries={inheritedEnv}
        editable={false}
        serviceName={serviceName}
        isCollapsible
      />
      <EnvSection
        title={t.service.mergedEnv}
        entries={mergedEnv}
        editable={false}
        serviceName={serviceName}
      />
    </div>
  )
}
