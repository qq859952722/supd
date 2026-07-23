// REQ-U-005: 环境变量编辑器
// 本服务env(可编辑)+继承env(折叠只读)+合并env(只读)+覆盖关系高亮
// 布局参照扩展 EnvTab：头部按钮(添加变量/显示敏感值/保存) + 列宽对齐 + 空行内联编辑
import { useState, useEffect } from 'react'
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
  const [showSecrets, setShowSecrets] = useState(false)
  const queryClient = useQueryClient()

  // 同步 props → editEntries：异步数据加载后更新编辑状态（修复 hint/enabled 值不显示）
  useEffect(() => {
    setEditEntries(entries)
  }, [entries])

  // E-02 修复：silent=true 避免与 onError 重复 toast，提取后端错误消息
  // 发送 config.EnvFile JSON 格式（{env:{KEY:{value,enabled?,hint?}}}），与后端 handleSaveServiceEnv 一致
  const saveMutation = useMutation({
    mutationFn: (envs: EnvEntry[]) => {
      // 过滤掉空 key 的行（未填写的新增行）
      const validEntries = envs.filter((e) => e.key.trim() !== '')
      // 本地 EnvEntry（enabled?/hint? 可选）→ lib EnvEntry（enabled/hint 必填）
      const libEntries = validEntries.map((e) => ({
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

  const handleSave = () => {
    saveMutation.mutate(editEntries)
  }

  const displayEntries = editable ? editEntries : entries

  return (
    <div>
      {/* 头部：标题 + 计数 + 操作按钮（参照扩展 EnvTab 布局） */}
      <div className="flex items-center gap-2 mb-3">
        {isCollapsible && (
          <button
            onClick={() => setCollapsed(!collapsed)}
            className="text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]"
          >
            {collapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
          </button>
        )}
        <h4 className="text-sm font-medium text-[var(--color-text-primary)]">{title}</h4>
        <Badge variant="secondary">{displayEntries.length}</Badge>
        {editable ? (
          <Badge variant="info">{t.service.editable}</Badge>
        ) : (
          <Badge variant="secondary">{t.service.readOnly}</Badge>
        )}
        {editable && (
          <div className="ml-auto flex items-center gap-2">
            <button
              onClick={() => setShowSecrets(!showSecrets)}
              className="text-xs text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)] underline"
            >
              {showSecrets ? '隐藏敏感值' : '显示敏感值'}
            </button>
            <Button
              variant="default"
              size="sm"
              onClick={() => setEditEntries([...editEntries, { key: '', value: '', source: 'service', enabled: true, hint: '' }])}
            >
              <Plus className="h-3.5 w-3.5" />
              添加变量
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={handleSave}
              disabled={saveMutation.isPending}
            >
              {saveMutation.isPending ? <Save className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
              {t.service.save}
            </Button>
          </div>
        )}
      </div>

      {!collapsed && (
        displayEntries.length === 0 ? (
          <div className="py-8 text-center text-sm text-[var(--color-text-tertiary)]">
            暂无环境变量{editable ? '，点击"添加变量"创建' : ''}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-48">Key</TableHead>
                <TableHead>Value</TableHead>
                {editable && <TableHead className="w-64">说明</TableHead>}
                {editable && <TableHead className="w-20">启用</TableHead>}
                <TableHead className="w-20">来源</TableHead>
                {editable && <TableHead className="w-12">操作</TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {displayEntries.map((entry, idx) => {
                const sensitive = isSensitiveKey(entry.key)
                return (
                  <TableRow key={idx} className={entry.overridden ? 'bg-[var(--color-surface-warning)]' : ''}>
                    {/* Key */}
                    <TableCell>
                      {editable ? (
                        <Input
                          value={entry.key}
                          onChange={(e) => {
                            const a = [...editEntries]; a[idx] = { ...entry, key: e.target.value }
                            setEditEntries(a)
                          }}
                          placeholder="VAR_NAME"
                          className="h-7 text-xs font-mono"
                        />
                      ) : (
                        <span className="font-mono text-sm">
                          {entry.key}
                          {entry.overridden && (
                            <Badge variant="warning" className="ml-2 text-[10px]">覆盖</Badge>
                          )}
                        </span>
                      )}
                    </TableCell>
                    {/* Value */}
                    <TableCell>
                      {editable ? (
                        <Input
                          type={sensitive && !showSecrets ? 'password' : 'text'}
                          value={entry.value}
                          onChange={(e) => {
                            const a = [...editEntries]; a[idx] = { ...entry, value: e.target.value }
                            setEditEntries(a)
                          }}
                          placeholder={sensitive ? '••••••' : 'value'}
                          className="h-7 text-xs font-mono"
                        />
                      ) : (
                        <span className="font-mono text-sm text-[var(--color-text-secondary)]">
                          {sensitive ? '••••••' : entry.value}
                        </span>
                      )}
                    </TableCell>
                    {/* 说明 (仅可编辑) */}
                    {editable && (
                      <TableCell>
                        <Input
                          value={entry.hint ?? ''}
                          onChange={(e) => {
                            const a = [...editEntries]; a[idx] = { ...entry, hint: e.target.value }
                            setEditEntries(a)
                          }}
                          placeholder="说明（可选）"
                          className="h-7 text-xs"
                        />
                      </TableCell>
                    )}
                    {/* 启用 (仅可编辑) */}
                    {editable && (
                      <TableCell>
                        <button
                          onClick={() => {
                            const a = [...editEntries]; a[idx] = { ...entry, enabled: !entry.enabled }
                            setEditEntries(a)
                          }}
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
                    {/* 来源 */}
                    <TableCell>
                      <Badge variant={entry.source === 'service' ? 'info' : 'secondary'}>
                        {entry.source === 'service' ? '本服务' : '继承'}
                      </Badge>
                    </TableCell>
                    {/* 操作 (仅可编辑) */}
                    {editable && (
                      <TableCell>
                        <Button
                          variant="danger"
                          size="sm"
                          onClick={() => setEditEntries(editEntries.filter((_, i) => i !== idx))}
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </TableCell>
                    )}
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )
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
