// REQ-U-005: 服务列表页
// 名称/状态/运行时长/重启次数/icon/tags
// tag过滤 / 批量操作：全部启动/全部停止

import { useState, useMemo, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost } from '@/lib/api-client'
import { useAuthStore } from '@/stores/auth'
import { t } from '@/lib/i18n'
import type { ServiceState, ServicesResponse } from '@/types/service'
import { ServiceCard } from '@/components/service/ServiceCard'
import { ServiceTable } from '@/components/service/ServiceTable'
import { ServiceForm, type ServiceConfig } from '@/components/service/ServiceForm'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Badge } from '@/components/ui/Badge'
import { Select } from '@/components/ui/Select'
import { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, DialogFooter, useDialog } from '@/components/ui/Dialog'
import { Play, Square, LayoutGrid, List, Filter, Plus, Upload, AlertTriangle, CheckCircle, Loader2 } from 'lucide-react'
import { toast } from '@/components/ui/Toast'
import { SkeletonTable } from '@/components/ui/Skeleton'
import { getErrorMessage } from '@/lib/error-utils'

// 导入预览响应（与后端 ImportPreviewResponse 对应）
interface ImportPreviewResponse {
  entries: string[]
  service_name?: string
  service_info?: {
    name: string
    archive_version: string
    local_version?: string
    exists_local: boolean
  }
  extensions?: Array<{
    name: string
    archive_version: string
    local_version?: string
    exists_local: boolean
  }>
  exists_local: boolean
}

// I-03-001 修复：ServiceItem / ServicesResponse 抽取到 @/types/service

// R-004 修复：ServiceForm 在 Dialog 内的包装组件，提供取消按钮关闭对话框
function CreateServiceForm({
  onSubmit,
  isLoading,
}: {
  onSubmit: (config: ServiceConfig) => void
  isLoading?: boolean
}) {
  const { setOpen } = useDialog()
  return (
    <ServiceForm
      onSubmit={onSubmit}
      onCancel={() => setOpen(false)}
      submitLabel="创建"
      isLoading={isLoading}
    />
  )
}

export function Services() {
  const queryClient = useQueryClient()
  const [viewMode, setViewMode] = useState<'table' | 'card'>('table')
  const [tagFilter, setTagFilter] = useState<string>('')
  const [searchQuery, setSearchQuery] = useState('')

  // E-01-001 修复：添加 isLoading 分支，首次加载显示骨架屏
  const { data, isLoading, isError } = useQuery({
    queryKey: ['services-list'],
    queryFn: () => apiGet<ServicesResponse>('/api/services'),
    refetchInterval: 5_000, // G-03: 服务列表短轮询 5s（降低高频请求压力）
  })

  const services = data?.services ?? []

  // 收集所有tag
  const allTags = useMemo(() => {
    const tagSet = new Set<string>()
    for (const svc of services) {
      for (const tag of svc.tags ?? []) {
        tagSet.add(tag)
      }
    }
    return Array.from(tagSet).sort()
  }, [services])

  // 稳定排序：活跃服务优先（ready > up > starting > stopping > failed > down > pending），同状态按名称排
  const statusOrder: Record<ServiceState, number> = {
    ready: 0, up: 1, starting: 2, stopping: 3, failed: 4, down: 5, pending: 6,
  }

  // 客户端过滤（REQ: 列表端点不提供过滤/排序参数，由客户端实现）
  const filteredServices = useMemo(() => {
    let result = services
    if (tagFilter) {
      result = result.filter((svc) => svc.tags?.includes(tagFilter))
    }
    if (searchQuery) {
      const q = searchQuery.toLowerCase()
      result = result.filter((svc) =>
        svc.name.toLowerCase().includes(q) ||
        svc.tags?.some((tag) => tag.toLowerCase().includes(q))
      )
    }
    return [...result].sort((a, b) => {
      const sa = statusOrder[a.status] ?? 9
      const sb = statusOrder[b.status] ?? 9
      if (sa !== sb) return sa - sb
      return a.name.localeCompare(b.name)
    })
  }, [services, tagFilter, searchQuery])

  // E-02-004 修复：silent=true 避免与 onError 重复 toast，提取后端错误消息
  const startAllMutation = useMutation({
    mutationFn: () => apiPost('/api/services/start', undefined, true),
    onSuccess: () => {
      toast.success('已发送启动全部指令')
      queryClient.invalidateQueries({ queryKey: ['services-list'] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '启动全部失败')) },
  })

  const stopAllMutation = useMutation({
    mutationFn: () => apiPost('/api/services/stop', undefined, true),
    onSuccess: () => {
      toast.success('已发送停止全部指令')
      queryClient.invalidateQueries({ queryKey: ['services-list'] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '停止全部失败')) },
  })

  const createServiceMutation = useMutation({
    mutationFn: (config: ServiceConfig) => {
      // E-09-002/F-01-001 修复：传递 ServiceForm 收集的完整配置
      return apiPost('/api/services', {
        name: config.name,
        command: config.command,
        version: config.version || undefined,
        description: config.description || undefined,
        icon: config.icon || undefined,
        workdir: config.workdir || undefined,
        runtime: config.runtime || undefined,
        user: config.user || undefined,
        group: config.group || undefined,
        depends_on: config.depends_on?.length ? config.depends_on : undefined,
        tags: config.tags?.length ? config.tags : undefined,
        autostart: config.autostart,
        readiness: config.readiness,
        restart: config.restart,
        stop: config.stop,
        logging: config.logging,
        signals: config.signals,
      }, true)
    },
    onSuccess: () => {
      toast.success('服务创建成功，重新扫描配置后生效')
      queryClient.invalidateQueries({ queryKey: ['services-list'] })
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '服务创建失败')) },
  })

  // --- 服务导入功能 ---
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [importFile, setImportFile] = useState<File | null>(null)
  const [importPreview, setImportPreview] = useState<ImportPreviewResponse | null>(null)
  const [showImportDialog, setShowImportDialog] = useState(false)

  // 上传文件预览导入内容
  const importPreviewMutation = useMutation({
    mutationFn: async (file: File) => {
      const formData = new FormData()
      formData.append('file', file)
      const token = useAuthStore.getState().token
      const headers: HeadersInit = {}
      if (token) headers['Authorization'] = `Bearer ${token}`
      const res = await fetch('/api/services/import', { method: 'POST', headers, body: formData })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body.message || `上传失败 (${res.status})`)
      }
      return res.json() as Promise<ImportPreviewResponse>
    },
    onSuccess: (data) => {
      setImportPreview(data)
      setShowImportDialog(true)
    },
    onError: (err: Error) => toast.error(err.message || '导入预览失败'),
  })

  // 确认导入
  const importConfirmMutation = useMutation({
    mutationFn: async ({ file, name }: { file: File; name: string }) => {
      const formData = new FormData()
      formData.append('file', file)
      formData.append('name', name)
      const token = useAuthStore.getState().token
      const headers: HeadersInit = {}
      if (token) headers['Authorization'] = `Bearer ${token}`
      const res = await fetch('/api/services/import/confirm', { method: 'POST', headers, body: formData })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body.message || `导入失败 (${res.status})`)
      }
      return res.json()
    },
    onSuccess: () => {
      toast.success('服务导入成功，重新扫描配置后生效')
      setShowImportDialog(false)
      setImportFile(null)
      setImportPreview(null)
      queryClient.invalidateQueries({ queryKey: ['services-list'] })
    },
    onError: (err: Error) => toast.error(err.message || '导入失败'),
  })

  const handleImportFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    if (!file.name.endsWith('.tar.gz') && !file.name.endsWith('.tgz')) {
      toast.error('请选择 .tar.gz 格式的文件')
      return
    }
    setImportFile(file)
    importPreviewMutation.mutate(file)
    // 重置 input 以便同一文件可再次选择
    e.target.value = ''
  }

  return (
    <div className="space-y-4">
      {/* 工具栏 */}
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-xl font-semibold text-[var(--color-text-primary)]">{t.service.list}</h2>
        <div className="flex-1" />
        <Input
          placeholder={t.common.search}
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          className="w-48"
        />
        <div className="flex items-center gap-2">
          <Filter className="h-4 w-4 text-[var(--color-text-tertiary)]" />
          <Select
            className="w-40"
            value={tagFilter}
            onChange={(e) => setTagFilter(e.target.value)}
            options={[
              { value: '', label: t.service.allTags },
              ...allTags.map((tag) => ({ value: tag, label: tag })),
            ]}
          />
        </div>
        <div className="flex items-center gap-1 rounded-lg bg-[var(--color-bg-tertiary)] p-1">
          <button
            onClick={() => setViewMode('table')}
            className={`rounded-md p-1.5 ${viewMode === 'table' ? 'bg-[var(--color-surface-primary)] shadow-sm text-[var(--color-text-primary)]' : 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}`}
          >
            <List className="h-4 w-4" />
          </button>
          <button
            onClick={() => setViewMode('card')}
            className={`rounded-md p-1.5 ${viewMode === 'card' ? 'bg-[var(--color-surface-primary)] shadow-sm text-[var(--color-text-primary)]' : 'text-[var(--color-text-tertiary)] hover:text-[var(--color-text-primary)]'}`}
          >
            <LayoutGrid className="h-4 w-4" />
          </button>
        </div>
        <Dialog>
          <DialogTrigger>
            <Button variant="primary" size="sm">
              <Plus className="h-3.5 w-3.5" />
              创建服务
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>创建服务</DialogTitle>
            </DialogHeader>
            <CreateServiceForm
              onSubmit={(config) => createServiceMutation.mutate(config)}
              isLoading={createServiceMutation.isPending}
            />
          </DialogContent>
        </Dialog>
        {/* 隐藏的文件输入用于导入 */}
        <input
          ref={fileInputRef}
          type="file"
          accept=".tar.gz,.tgz"
          className="hidden"
          onChange={handleImportFileSelect}
        />
        <Button
          variant="default"
          size="sm"
          onClick={() => fileInputRef.current?.click()}
          disabled={importPreviewMutation.isPending}
        >
          {importPreviewMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
          导入服务
        </Button>
        <Button
          variant="primary"
          size="sm"
          onClick={() => startAllMutation.mutate()}
          disabled={startAllMutation.isPending}
        >
          <Play className="h-3.5 w-3.5" />
          {t.service.startAll}
        </Button>
        <Button
          variant="danger"
          size="sm"
          onClick={() => stopAllMutation.mutate()}
          disabled={stopAllMutation.isPending}
        >
          <Square className="h-3.5 w-3.5" />
          {t.service.stopAll}
        </Button>
      </div>

      {/* E-02-001 修复：API 错误时显示错误横幅，避免误以为无服务 */}
      {isError && (
        <div className="flex items-center gap-2 rounded-md border border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-3 py-2 text-sm text-[var(--color-text-error)]">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span>服务列表加载失败，将在稍后自动重试。</span>
        </div>
      )}

      {/* 内容区 — E-01-001 修复：首次加载显示骨架屏 */}
      {isLoading ? (
        <SkeletonTable />
      ) : viewMode === 'table' ? (
        <ServiceTable services={filteredServices} />
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {filteredServices.map((svc) => (
            <ServiceCard key={svc.name} {...svc} />
          ))}
        </div>
      )}

      {/* 导入版本对比确认弹窗 */}
      {showImportDialog && importPreview && importFile && (
        <Dialog defaultOpen>
          <DialogContent className="max-w-lg">
            <DialogHeader>
              <DialogTitle>导入服务 — 版本对比</DialogTitle>
            </DialogHeader>
            <div className="space-y-4 py-2">
              {/* 服务版本对比 */}
              {importPreview.service_info && (
                <div className="rounded-lg border border-[var(--color-border-secondary)] p-3">
                  <div className="flex items-center gap-2 mb-2">
                    <span className="text-sm font-medium">服务</span>
                    <Badge variant="info">{importPreview.service_info.name}</Badge>
                    {importPreview.service_info.exists_local ? (
                      <Badge variant="warning">本地已存在</Badge>
                    ) : (
                      <Badge variant="success">新增</Badge>
                    )}
                  </div>
                  <div className="flex items-center gap-3 text-sm">
                    <div>
                      <span className="text-[var(--color-text-tertiary)]">压缩包版本：</span>
                      <span className="font-mono">{importPreview.service_info.archive_version || '-'}</span>
                    </div>
                    {importPreview.service_info.exists_local && (
                      <div>
                        <span className="text-[var(--color-text-tertiary)]">本地版本：</span>
                        <span className="font-mono">{importPreview.service_info.local_version || '-'}</span>
                      </div>
                    )}
                  </div>
                  {importPreview.service_info.exists_local && (
                    <div className="mt-2 flex items-center gap-1.5 text-xs text-[var(--color-warning)]">
                      <AlertTriangle className="h-3.5 w-3.5" />
                      导入将覆盖本地服务配置（data/ 目录保留）
                    </div>
                  )}
                </div>
              )}

              {/* 扩展版本对比 */}
              {importPreview.extensions && importPreview.extensions.length > 0 && (
                <div className="rounded-lg border border-[var(--color-border-secondary)] p-3">
                  <div className="text-sm font-medium mb-2">包含的扩展</div>
                  <div className="space-y-1.5">
                    {importPreview.extensions.map((ext) => (
                      <div key={ext.name} className="flex items-center gap-2 text-sm">
                        <span className="font-mono">{ext.name}</span>
                        <span className="text-[var(--color-text-tertiary)]">v{ext.archive_version || '-'}</span>
                        {ext.exists_local ? (
                          <>
                            <span className="text-[var(--color-text-tertiary)]">→本地 v{ext.local_version || '-'}</span>
                            {ext.archive_version !== ext.local_version ? (
                              <Badge variant="warning">版本不同</Badge>
                            ) : (
                              <Badge variant="success">版本一致</Badge>
                            )}
                          </>
                        ) : (
                          <Badge variant="info">新增扩展</Badge>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* 文件列表摘要 */}
              <div className="text-xs text-[var(--color-text-tertiary)]">
                共 {importPreview.entries.length} 个文件，文件名：{importFile.name}
              </div>
            </div>
            <DialogFooter>
              <Button variant="default" size="sm" onClick={() => { setShowImportDialog(false); setImportFile(null); setImportPreview(null) }}>
                取消
              </Button>
              <Button
                variant="primary"
                size="sm"
                onClick={() => {
                  if (importPreview.service_name) {
                    importConfirmMutation.mutate({ file: importFile, name: importPreview.service_name })
                  }
                }}
                disabled={importConfirmMutation.isPending || !importPreview.service_name}
              >
                {importConfirmMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CheckCircle className="h-3.5 w-3.5" />}
                确认导入
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}
