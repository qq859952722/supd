// REQ-U-015: 多标签编辑器
// 最多8个标签（数值锁定REQ-2.9.9）、localStorage持久化、标签切换+关闭
// REQ-2.3.1: 版本历史（50个版本）+ 回滚
// 支持右键菜单：关闭/关闭其他/关闭全部/关闭右侧

import { useCallback, useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { X, FileText, History, RotateCcw } from 'lucide-react'
import { useEditorStore, type EditorTab } from '@/stores/editor'
import { MonacoEditor } from './MonacoEditor'
import { Button } from '@/components/ui/Button'
import { apiGet, apiPut, apiPost } from '@/lib/api-client'
import { toast } from '@/components/ui/Toast'
import { t } from '@/lib/i18n'
import { getErrorMessage } from '@/lib/error-utils'

// FileVersion 与后端 FileVersion 结构体对应（字段名对齐）
interface FileVersion {
  version: number
  timestamp: string
  size: number
}

interface TabContextMenuState {
  x: number
  y: number
  tab: EditorTab
}

export function EditorTabs() {
  const { tabs, activeTabId, closeTab, closeOtherTabs, closeAllTabs, closeTabsToRight, setActiveTab, updateTabContent, markTabSaved } = useEditorStore()
  const queryClient = useQueryClient()
  const [showVersions, setShowVersions] = useState(false)
  const [contextMenu, setContextMenu] = useState<TabContextMenuState | null>(null)

  const activeTab = tabs.find((tab) => tab.id === activeTabId) ?? null

  // REQ-2.3.1: 当前激活标签的版本历史
  const { data: versions } = useQuery({
    queryKey: ['file-versions', activeTab?.path],
    queryFn: () => apiGet<FileVersion[]>('/api/files/history', { path: activeTab!.path }),
    enabled: !!activeTab && showVersions,
  })

  // E-04-003 修复：Ctrl+S 保存当前标签到后端 API
  const handleSave = useCallback(
    async (tabId: string, content: string) => {
      const tab = useEditorStore.getState().tabs.find((item) => item.id === tabId)
      if (!tab) return
      // 先同步本地内容（保证 UI 即时反馈）
      updateTabContent(tabId, content)
      try {
        // silent=true 抑制全局 toast，由本地回调控制提示
        await apiPut('/api/files?path=' + encodeURIComponent(tab.path), { content }, true)
        markTabSaved(tabId)
        toast.success(`已保存：${tab.name}`)
      } catch {
        toast.error(`保存失败：${tab.name}`)
      }
    },
    [updateTabContent, markTabSaved],
  )

  // REQ-2.3.1: 版本回滚
  const rollbackMutation = useMutation({
    mutationFn: ({ path, version }: { path: string; version: number }) =>
      apiPost('/api/files/rollback?path=' + encodeURIComponent(path), { version }, true),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ['file-versions', variables.path] })
      // 重新拉取文件内容并同步到标签
      apiGet<{ path: string; content: string }>('/api/files', { path: variables.path }).then((res) => {
        const tab = useEditorStore.getState().tabs.find((item) => item.path === variables.path)
        if (tab) {
          updateTabContent(tab.id, res.content)
          markTabSaved(tab.id)
        }
      })
      toast.success('已回滚到指定版本')
    },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '版本回滚失败')) },
  })

  // E-03-002: 关闭 dirty 标签时弹出确认提示（非阻塞 toast 浮窗，不用全屏 modal）
  const handleCloseTab = useCallback(
    (tab: EditorTab) => {
      if (tab.isDirty) {
        toast.warning(`"${tab.name}" 有未保存的修改`, {
          action: {
            label: '仍要关闭',
            onClick: () => closeTab(tab.id),
          },
          duration: 6000,
        })
        return
      }
      closeTab(tab.id)
    },
    [closeTab],
  )

  // 切换标签时关闭版本面板，避免跨文件混淆
  const handleSwitchTab = useCallback(
    (tabId: string) => {
      setActiveTab(tabId)
      setShowVersions(false)
    },
    [setActiveTab],
  )

  // 关闭菜单点击外部关闭
  useEffect(() => {
    if (!contextMenu) return
    const handler = () => setContextMenu(null)
    window.addEventListener('click', handler)
    return () => window.removeEventListener('click', handler)
  }, [contextMenu])

  // 批量关闭时检查 dirty 标签
  const handleBatchClose = useCallback(
    (action: 'close' | 'closeOthers' | 'closeAll' | 'closeRight', tab: EditorTab) => {
      let dirtyTabs: EditorTab[] = []
      switch (action) {
        case 'close':
          handleCloseTab(tab)
          return
        case 'closeOthers':
          dirtyTabs = tabs.filter((t) => t.id !== tab.id && t.isDirty)
          break
        case 'closeAll':
          dirtyTabs = tabs.filter((t) => t.isDirty)
          break
        case 'closeRight':
          const idx = tabs.findIndex((t) => t.id === tab.id)
          dirtyTabs = tabs.slice(idx + 1).filter((t) => t.isDirty)
          break
      }
      if (dirtyTabs.length > 0) {
        toast.warning(`${dirtyTabs.length} 个标签有未保存的修改`, {
          action: {
            label: '仍要关闭',
            onClick: () => {
              switch (action) {
                case 'closeOthers': closeOtherTabs(tab.id); break
                case 'closeAll': closeAllTabs(); break
                case 'closeRight': closeTabsToRight(tab.id); break
              }
            },
          },
          duration: 6000,
        })
        return
      }
      switch (action) {
        case 'closeOthers': closeOtherTabs(tab.id); break
        case 'closeAll': closeAllTabs(); break
        case 'closeRight': closeTabsToRight(tab.id); break
      }
    },
    [tabs, closeOtherTabs, closeAllTabs, closeTabsToRight, handleCloseTab],
  )

  if (tabs.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-[var(--color-text-tertiary)]">
        <p className="text-sm">打开文件以开始编辑</p>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      {/* 标签栏 + 工具按钮 */}
      <div className="flex shrink-0 items-stretch border-b border-[var(--color-border-primary)] bg-[var(--color-bg-secondary)]">
        {/* 标签列表 */}
        <div className="flex flex-1 overflow-x-auto">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => handleSwitchTab(tab.id)}
              onContextMenu={(e) => {
                e.preventDefault()
                setContextMenu({ x: e.clientX, y: e.clientY, tab })
              }}
              className={`group flex shrink-0 items-center gap-1.5 border-r border-[var(--color-border-primary)] px-3 py-1.5 text-xs transition-colors ${
                tab.id === activeTabId
                  ? 'bg-[var(--color-bg-primary)] text-[var(--color-text-primary)]'
                  : 'text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-secondary)]'
              }`}
            >
              <FileText className="h-3 w-3 shrink-0" />
              <span className="max-w-[120px] truncate">{tab.name}</span>
              {tab.isDirty && <span className="h-1.5 w-1.5 rounded-full bg-[var(--color-brand-primary)]" />}
              <span
                onClick={(e) => {
                  e.stopPropagation()
                  handleCloseTab(tab)
                }}
                className="ml-1 rounded p-0.5 opacity-0 hover:bg-[var(--color-surface-hover)] group-hover:opacity-100 transition-opacity"
              >
                <X className="h-3 w-3" />
              </span>
            </button>
          ))}
        </div>
        {/* 版本历史按钮（基于当前激活标签） */}
        {activeTab && (
          <button
            onClick={() => setShowVersions((prev) => !prev)}
            className={`flex shrink-0 items-center gap-1 border-l border-[var(--color-border-primary)] px-3 py-1.5 text-xs transition-colors ${
              showVersions
                ? 'bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)]'
                : 'text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-secondary)]'
            }`}
            title={t.files.versions}
          >
            <History className="h-3.5 w-3.5" />
            <span>{t.files.versions}</span>
          </button>
        )}
        {/* E-04-001: 标签数量指示器，接近上限时高亮提示 */}
        <span
          className={`flex shrink-0 items-center border-l border-[var(--color-border-primary)] px-3 py-1.5 text-xs tabular-nums ${
            tabs.length >= 8
              ? 'text-[var(--color-text-error)] font-medium'
              : tabs.length >= 6
                ? 'text-[var(--color-accent-warning)]'
                : 'text-[var(--color-text-tertiary)]'
          }`}
          title={`标签页 ${tabs.length}/8`}
        >
          {tabs.length}/8
        </span>
      </div>

      {/* 编辑器区域 */}
      {activeTab && (
        <div className="flex-1 overflow-hidden">
          <MonacoEditor
            value={activeTab.content}
            onChange={(value) => updateTabContent(activeTab.id, value)}
            onSave={(value) => handleSave(activeTab.id, value)}
            filename={activeTab.path}
          />
        </div>
      )}

      {/* REQ-2.3.1: 版本历史面板（底部展开） */}
      {showVersions && activeTab && (
        <div className="shrink-0 border-t border-[var(--color-border-primary)] max-h-60 overflow-auto bg-[var(--color-bg-primary)]">
          <div className="p-3 space-y-2">
            <h4 className="text-sm font-medium text-[var(--color-text-primary)]">{t.files.versionHistory}</h4>
            {versions && versions.length > 0 ? (
              versions.map((v) => (
                <div
                  key={v.version}
                  className="flex items-center justify-between rounded border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-2 text-xs"
                >
                  <div className="min-w-0">
                    <span className="font-medium text-[var(--color-text-primary)]">v{v.version}</span>
                    <span className="ml-2 text-[var(--color-text-tertiary)]">
                      {v.timestamp ? new Date(v.timestamp).toLocaleString('zh-CN') : '-'}
                    </span>
                    <span className="ml-2 text-[var(--color-text-tertiary)]">{(v.size / 1024).toFixed(1)}KB</span>
                  </div>
                  <Button
                    variant="default"
                    size="sm"
                    onClick={() =>
                      rollbackMutation.mutate({ path: activeTab.path, version: v.version })
                    }
                    disabled={rollbackMutation.isPending}
                  >
                    <RotateCcw className="h-3 w-3" />
                    {t.files.rollback}
                  </Button>
                </div>
              ))
            ) : (
              <p className="text-xs text-[var(--color-text-tertiary)]">{t.files.noVersions}</p>
            )}
          </div>
        </div>
      )}

      {/* Tab 右键菜单 */}
      {contextMenu && (
        <div
          className="fixed z-50 min-w-[130px] rounded-md border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] py-1 shadow-lg"
          style={{ left: contextMenu.x, top: contextMenu.y }}
          onClick={(e) => e.stopPropagation()}
          onContextMenu={(e) => e.preventDefault()}
        >
          <button
            className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
            onClick={() => {
              handleBatchClose('close', contextMenu.tab)
              setContextMenu(null)
            }}
          >
            <X className="h-3 w-3" />
            关闭
          </button>
          <button
            className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
            onClick={() => {
              handleBatchClose('closeOthers', contextMenu.tab)
              setContextMenu(null)
            }}
          >
            关闭其他
          </button>
          <button
            className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
            onClick={() => {
              handleBatchClose('closeRight', contextMenu.tab)
              setContextMenu(null)
            }}
          >
            关闭右侧
          </button>
          <div className="my-1 border-t border-[var(--color-border-primary)]" />
          <button
            className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-error)] hover:bg-[var(--color-surface-error)]"
            onClick={() => {
              handleBatchClose('closeAll', contextMenu.tab)
              setContextMenu(null)
            }}
          >
            关闭全部
          </button>
        </div>
      )}
    </div>
  )
}
