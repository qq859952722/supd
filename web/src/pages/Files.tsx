// REQ-U-010: 文件管理页面
// 左侧文件树 + 右侧多标签编辑器（EditorTabs）+ 文件操作
// 支持右键菜单：新建文件/文件夹、重命名、删除

import { useState, useEffect, useRef } from 'react'
import { useSearchParams } from 'react-router'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost, apiDelete } from '@/lib/api-client'
import { useAuthStore } from '@/stores/auth'
import { toast } from '@/components/ui/Toast'
import { FileTree, type FileNode, type FileTreeActions } from '@/components/file/FileTree'
import { EditorTabs } from '@/components/editor/EditorTabs'
import { useEditorStore } from '@/stores/editor'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { t } from '@/lib/i18n'
import { Plus, Trash2, RefreshCw, AlertTriangle, Upload } from 'lucide-react'
import { getErrorMessage } from '@/lib/error-utils'

type CreateMode = 'file' | 'folder' | null

export default function FilesPage() {
  const queryClient = useQueryClient()
  const [searchParams] = useSearchParams()
  const pathFromUrl = searchParams.get('path')
  const [selectedPath, setSelectedPath] = useState<string | null>(pathFromUrl)
  // 新建对话框状态
  const [createMode, setCreateMode] = useState<CreateMode>(null)
  const [createParentDir, setCreateParentDir] = useState('')
  const [createName, setCreateName] = useState('')
  // 重命名对话框状态
  const [renameNode, setRenameNode] = useState<FileNode | null>(null)
  const [renameName, setRenameName] = useState('')
  // E-09-001: 删除前内联二次确认（非阻塞，不可恢复操作）
  const [confirmingDelete, setConfirmingDelete] = useState<FileNode | null>(null)
  // 上传文件：隐藏 input + 目标目录
  const uploadInputRef = useRef<HTMLInputElement>(null)
  const [uploadTargetDir, setUploadTargetDir] = useState('')

  // E-07-001: 全局搜索跳转 /files?path=... 时同步选中文件
  useEffect(() => {
    if (pathFromUrl && pathFromUrl !== selectedPath) {
      setSelectedPath(pathFromUrl)
    }
  }, [pathFromUrl]) // eslint-disable-line react-hooks/exhaustive-deps

  // E-04-001: 选中文件时加载内容并打开为编辑器标签（最多8个，localStorage持久化）
  // 大文件（>1MB）打开前提示确认
  const LARGE_FILE_THRESHOLD = 1 * 1024 * 1024 // 1MB
  useEffect(() => {
    if (!selectedPath) return
    // 查找文件节点获取 size
    const findNode = (nodes: FileNode[], path: string): FileNode | null => {
      for (const n of nodes) {
        if (n.path === path) return n
        if (n.children) {
          const found = findNode(n.children, path)
          if (found) return found
        }
      }
      return null
    }
    const node = treeData ? findNode(treeData, selectedPath) : null
    if (node && node.size && node.size > LARGE_FILE_THRESHOLD) {
      const sizeMB = (node.size / 1024 / 1024).toFixed(1)
      toast.warning(`文件较大 (${sizeMB}MB)，打开可能较慢`, {
        action: {
          label: '仍要打开',
          onClick: () => loadFileContent(selectedPath),
        },
        duration: 8000,
      })
      return
    }
    loadFileContent(selectedPath)
  }, [selectedPath]) // eslint-disable-line react-hooks/exhaustive-deps

  function loadFileContent(path: string) {
    apiGet<{ path: string; content: string }>('/api/files', { path }, true)
      .then((res) => {
        const name = path.split('/').pop() || path
        useEditorStore.getState().openTab({
          id: path,
          name,
          path,
          content: res.content,
        })
      })
      .catch(() => {
        // silent — 错误由 api-client 统一处理
      })
  }

  // E-01 修复：处理 isError 错误状态，避免错误时静默空白
  const { data: treeData, isLoading, isError, error } = useQuery({
    queryKey: ['file-tree'],
    queryFn: () => apiGet<FileNode[]>('/api/files/tree'),
  })

  // 新建文件/文件夹
  // E-02 修复：silent=true 避免与 onError 重复 toast，提取后端错误消息
  const createMutation = useMutation({
    mutationFn: ({ path, isDir }: { path: string; isDir: boolean }) =>
      apiPost('/api/files?path=' + encodeURIComponent(path), { content: '', is_dir: isDir }, true),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['file-tree'] })
      setCreateMode(null)
      setCreateName('')
      toast.success('创建成功')
    },
    onError: (err: unknown) => {
      toast.error(getErrorMessage(err, '创建失败'))
    },
  })

  // 删除文件/文件夹
  // E-02 修复：silent=true 避免与 onError 重复 toast，提取后端错误消息
  const deleteMutation = useMutation({
    mutationFn: (path: string) => apiDelete('/api/files?path=' + encodeURIComponent(path), true),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['file-tree'] })
      setConfirmingDelete(null)
      toast.success('删除成功')
    },
    onError: (err: unknown) => {
      setConfirmingDelete(null)
      toast.error(getErrorMessage(err, '删除失败'))
    },
  })

  // 重命名（移动文件）
  // E-02 修复：silent=true 避免与 onError 重复 toast，提取后端错误消息
  const renameMutation = useMutation({
    mutationFn: ({ oldPath, newPath }: { oldPath: string; newPath: string }) =>
      apiPost('/api/files/move?path=' + encodeURIComponent(oldPath), { destination: newPath }, true),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['file-tree'] })
      setRenameNode(null)
      setRenameName('')
      toast.success('重命名成功')
    },
    onError: (err: unknown) => {
      toast.error(getErrorMessage(err, '重命名失败'))
    },
  })

  // 上传文件：multipart/form-data → POST /api/files/upload?path=目标目录
  const uploadMutation = useMutation({
    mutationFn: async ({ file, targetDir }: { file: File; targetDir: string }) => {
      const formData = new FormData()
      formData.append('file', file)
      const token = useAuthStore.getState().token
      const headers: HeadersInit = {}
      if (token) headers['Authorization'] = `Bearer ${token}`
      const res = await fetch('/api/files/upload?path=' + encodeURIComponent(targetDir), {
        method: 'POST',
        headers,
        body: formData,
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body.message || `上传失败 (${res.status})`)
      }
      return res.json()
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['file-tree'] })
      toast.success('上传成功')
    },
    onError: (err: Error) => toast.error(err.message || '上传失败'),
  })

  const handleUploadFileSelect = () => {
    const input = uploadInputRef.current
    const file = input?.files?.[0]
    if (!file) return
    uploadMutation.mutate({ file, targetDir: uploadTargetDir })
    // 清空 input value 以便重复选择同一文件
    if (input) input.value = ''
  }

  // 文件树右键菜单 actions
  const fileTreeActions: FileTreeActions = {
    onNewFile: (parentDir) => {
      setCreateMode('file')
      setCreateParentDir(parentDir)
      setCreateName('')
    },
    onNewFolder: (parentDir) => {
      setCreateMode('folder')
      setCreateParentDir(parentDir)
      setCreateName('')
    },
    onRename: (node) => {
      setRenameNode(node)
      setRenameName(node.name)
    },
    onDelete: (node) => {
      setConfirmingDelete(node)
    },
    onCopyPath: (node) => {
      navigator.clipboard.writeText(node.path).then(() => {
        toast.success(`已复制: ${node.path}`)
      }).catch(() => {
        toast.info(`路径: ${node.path}`)
      })
    },
    onUpload: (targetDir) => {
      setUploadTargetDir(targetDir)
      // 异步触发以使 state 先生效
      setTimeout(() => uploadInputRef.current?.click(), 0)
    },
  }

  // 构建新建路径
  const buildCreatePath = (): string => {
    if (createParentDir) {
      return createParentDir + '/' + createName
    }
    return createName
  }

  // 构建重命名路径
  const buildRenamePath = (): string => {
    if (!renameNode) return ''
    const parent = renameNode.path.substring(0, renameNode.path.lastIndexOf('/'))
    return parent ? parent + '/' + renameName : renameName
  }

  return (
    <div className="flex h-[calc(100vh-8rem)] gap-4">
      {/* 左侧文件树 */}
      <div className="w-64 shrink-0 flex flex-col border border-[var(--color-border-primary)] rounded-lg bg-[var(--color-surface-secondary)]">
        <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-3 py-2">
          <span className="text-sm font-medium text-[var(--color-text-primary)]">{t.files.title}</span>
          <div className="flex gap-1">
            <Button
              variant="default"
              size="sm"
              className="h-7 px-1.5"
              onClick={() => {
                setCreateMode('file')
                setCreateParentDir('')
                setCreateName('')
              }}
              title="新建文件"
            >
              <Plus className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="default"
              size="sm"
              className="h-7 px-1.5"
              onClick={() => {
                setUploadTargetDir('')
                setTimeout(() => uploadInputRef.current?.click(), 0)
              }}
              title="上传到根目录"
              disabled={uploadMutation.isPending}
            >
              <Upload className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="default"
              size="sm"
              className="h-7 px-1.5"
              onClick={() => queryClient.invalidateQueries({ queryKey: ['file-tree'] })}
              title="刷新"
            >
              <RefreshCw className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        <div className="flex-1 overflow-auto p-2">
          {isError ? (
            <div className="flex flex-col items-center gap-2 py-4 text-center">
              <AlertTriangle className="h-4 w-4 text-[var(--color-text-error)]" />
              <div className="text-xs text-[var(--color-text-error)]">
                加载失败: {error instanceof Error ? error.message : '未知错误'}
              </div>
              <button
                onClick={() => queryClient.invalidateQueries({ queryKey: ['file-tree'] })}
                className="text-xs text-[var(--color-brand-primary)] underline hover:opacity-80"
              >
                重试
              </button>
            </div>
          ) : isLoading ? (
            <div className="text-xs text-[var(--color-text-tertiary)]">{t.common.loading}</div>
          ) : treeData && treeData.length > 0 ? (
            <FileTree
              nodes={treeData}
              selectedPath={selectedPath ?? undefined}
              onSelect={setSelectedPath}
              actions={fileTreeActions}
            />
          ) : (
            <div className="text-xs text-[var(--color-text-tertiary)] text-center py-4">{t.files.empty}</div>
          )}
        </div>
      </div>

      {/* 右侧多标签编辑器 — E-04-001: 接入 EditorTabs（§2.9.9 最多8个标签 + localStorage 持久化） */}
      <div className="flex-1 border border-[var(--color-border-primary)] rounded-lg bg-[var(--color-surface-secondary)] overflow-hidden">
        <EditorTabs />
      </div>

      {/* E-09 修复：新建文件/文件夹 — 非阻塞浮动 Dialog，动作按钮在 header */}
      {createMode && (
        <div className="fixed top-16 left-1/2 z-50 w-96 -translate-x-1/2 rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-primary)] shadow-xl">
          <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-3 py-2">
            <span className="text-sm font-medium text-[var(--color-text-primary)]">
              {createMode === 'file' ? '新建文件' : '新建文件夹'}
            </span>
            <div className="flex gap-1">
              <Button variant="default" size="sm" onClick={() => setCreateMode(null)}>取消</Button>
              <Button
                variant="primary"
                size="sm"
                disabled={!createName}
                onClick={() => createMutation.mutate({ path: buildCreatePath(), isDir: createMode === 'folder' })}
              >
                创建
              </Button>
            </div>
          </div>
          <div className="p-3">
            {createParentDir && (
              <p className="mb-2 text-xs text-[var(--color-text-tertiary)]">位置: {createParentDir}</p>
            )}
            <Input
              value={createName}
              onChange={(e) => setCreateName(e.target.value)}
              placeholder={createMode === 'file' ? '文件名.yaml' : '文件夹名'}
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter' && createName) {
                  createMutation.mutate({ path: buildCreatePath(), isDir: createMode === 'folder' })
                }
                if (e.key === 'Escape') setCreateMode(null)
              }}
            />
          </div>
        </div>
      )}

      {/* E-09 修复：重命名 — 非阻塞浮动 Dialog，动作按钮在 header */}
      {renameNode && (
        <div className="fixed top-16 left-1/2 z-50 w-96 -translate-x-1/2 rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-primary)] shadow-xl">
          <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-3 py-2">
            <span className="text-sm font-medium text-[var(--color-text-primary)]">重命名</span>
            <div className="flex gap-1">
              <Button variant="default" size="sm" onClick={() => setRenameNode(null)}>取消</Button>
              <Button
                variant="primary"
                size="sm"
                disabled={!renameName || renameName === renameNode.name}
                onClick={() => renameMutation.mutate({ oldPath: renameNode.path, newPath: buildRenamePath() })}
              >
                重命名
              </Button>
            </div>
          </div>
          <div className="p-3">
            <Input
              value={renameName}
              onChange={(e) => setRenameName(e.target.value)}
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter' && renameName) {
                  renameMutation.mutate({ oldPath: renameNode.path, newPath: buildRenamePath() })
                }
                if (e.key === 'Escape') setRenameNode(null)
              }}
            />
          </div>
        </div>
      )}

      {/* E-09 修复：删除确认 — 非阻塞浮动 Dialog，动作按钮在 header */}
      {confirmingDelete && (
        <div className="fixed top-16 left-1/2 z-50 w-96 -translate-x-1/2 rounded-lg border border-[var(--color-border-error)] bg-[var(--color-surface-primary)] shadow-xl">
          <div className="flex items-center justify-between border-b border-[var(--color-border-primary)] px-3 py-2">
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 text-[var(--color-text-error)]" />
              <span className="text-sm font-medium text-[var(--color-text-primary)]">确认删除？</span>
            </div>
            <div className="flex gap-1">
              <Button variant="default" size="sm" onClick={() => setConfirmingDelete(null)}>取消</Button>
              <Button
                variant="danger"
                size="sm"
                disabled={deleteMutation.isPending}
                onClick={() => deleteMutation.mutate(confirmingDelete.path)}
              >
                <Trash2 className="h-3.5 w-3.5" />
                删除
              </Button>
            </div>
          </div>
          <div className="p-3">
            <p className="text-xs text-[var(--color-text-secondary)]">
              {confirmingDelete.is_dir ? '文件夹' : '文件'}: {confirmingDelete.name}
            </p>
            <p className="mt-1 text-xs text-[var(--color-text-tertiary)]">此操作不可恢复</p>
          </div>
        </div>
      )}

      {/* 隐藏的文件上传 input（由右键菜单或工具栏按钮触发） */}
      <input
        ref={uploadInputRef}
        type="file"
        className="hidden"
        onChange={handleUploadFileSelect}
      />
    </div>
  )
}
