// REQ-U-011: 全局环境变量编辑器
// 编辑全局env配置

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPut } from '@/lib/api-client'
import { toast } from '@/components/ui/Toast'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { t } from '@/lib/i18n'
import { Save, Plus, Trash2 } from 'lucide-react'
import { Input } from '@/components/ui/Input'

interface EnvEntry {
  key: string
  value: string
}

// 后端 /api/settings/env 返回 {env: {KEY: {value, enabled?, hint?}}}
interface EnvFileResponse {
  env: Record<string, { value: string; enabled?: boolean; hint?: string }>
}

export function EnvEditor() {
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
    }))
    setEntries(envEntries)
    setInitialized(true)
  }

  const saveMutation = useMutation({
    mutationFn: (env: EnvEntry[]) => {
      const envMap: Record<string, { value: string }> = {}
      for (const e of env) {
        if (e.key) envMap[e.key] = { value: e.value }
      }
      return apiPut('/api/settings/env', { env: envMap }, true)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-env'] })
      toast.success('环境变量已保存')
    },
    onError: () => {
      // E-02-001: 环境变量保存失败时提示用户
      toast.error('环境变量保存失败')
    },
  })

  function addEntry() {
    setEntries([...entries, { key: '', value: '' }])
  }

  function removeEntry(index: number) {
    setEntries(entries.filter((_, i) => i !== index))
  }

  function updateEntry(index: number, field: 'key' | 'value', val: string) {
    const updated = [...entries]
    updated[index] = { ...updated[index]!, [field]: val }
    setEntries(updated)
  }

  // API 返回 500 时显示空状态提示
  if (error) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{t.settings.globalEnv}</CardTitle>
          <CardDescription>{t.settings.globalEnvDesc}</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-[var(--color-text-secondary)]">环境变量文件不存在，请先创建配置文件后重试。</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t.settings.globalEnv}</CardTitle>
        <CardDescription>{t.settings.globalEnvDesc}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {entries.map((entry, index) => (
          <div key={index} className="flex items-center gap-2">
            <Input
              value={entry.key}
              onChange={(e) => updateEntry(index, 'key', e.target.value)}
              placeholder={t.settings.envKey}
              className="flex-1"
            />
            <Input
              value={entry.value}
              onChange={(e) => updateEntry(index, 'value', e.target.value)}
              placeholder={t.settings.envValue}
              className="flex-1"
            />
            <Button variant="danger" size="sm" onClick={() => removeEntry(index)}>
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
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
            <Save className="h-4 w-4" />
            {t.settings.saveEnv}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
