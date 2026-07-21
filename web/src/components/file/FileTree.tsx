// REQ-U-010: 文件树组件
// /etc/supd/目录下的白名单路径文件树
// 支持右键菜单：新建文件/文件夹、重命名、删除

import { useState, useCallback, useEffect } from 'react'
import { ChevronRight, ChevronDown, File, Folder, FolderOpen } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface FileNode {
  name: string
  path: string
  is_dir: boolean
  size?: number
  children?: FileNode[]
}

interface ContextMenuState {
  x: number
  y: number
  node: FileNode | null  // null = root context
}

export interface FileTreeActions {
  onNewFile: (parentDir: string) => void
  onNewFolder: (parentDir: string) => void
  onRename: (node: FileNode) => void
  onDelete: (node: FileNode) => void
  onCopyPath: (node: FileNode) => void
  onUpload: (targetDir: string) => void
}

export function FileTree({
  nodes,
  selectedPath,
  onSelect,
  actions,
}: {
  nodes: FileNode[]
  selectedPath?: string
  onSelect: (path: string) => void
  actions?: FileTreeActions
}) {
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null)

  const closeContextMenu = useCallback(() => setContextMenu(null), [])

  // 右键空白处 → root 上下文
  const handleContainerContextMenu = useCallback(
    (e: React.MouseEvent) => {
      if (!actions) return
      e.preventDefault()
      setContextMenu({ x: e.clientX, y: e.clientY, node: null })
    },
    [actions],
  )

  // 点击任意位置关闭菜单
  // 注意：不监听 contextmenu，否则第二次右键会关闭菜单而非重新定位
  useEffect(() => {
    if (!contextMenu) return
    const handler = () => closeContextMenu()
    window.addEventListener('click', handler)
    return () => {
      window.removeEventListener('click', handler)
    }
  }, [contextMenu, closeContextMenu])

  // 获取右键菜单操作的父目录
  const getTargetDir = (node: FileNode | null): string => {
    if (!node) return ''
    return node.is_dir ? node.path : node.path.substring(0, node.path.lastIndexOf('/'))
  }

  return (
    <div className="text-sm" onContextMenu={handleContainerContextMenu}>
      {nodes.map((node) => (
        <FileTreeNode
          key={node.path}
          node={node}
          depth={0}
          selectedPath={selectedPath}
          onSelect={onSelect}
          actions={actions}
          onContextMenu={(e, node) => {
            e.preventDefault()
            e.stopPropagation()
            setContextMenu({ x: e.clientX, y: e.clientY, node })
          }}
        />
      ))}

      {/* 右键菜单 */}
      {contextMenu && actions && (
        <div
          className="fixed z-50 min-w-[140px] rounded-md border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] py-1 shadow-lg"
          style={{ left: contextMenu.x, top: contextMenu.y }}
          onClick={(e) => e.stopPropagation()}
          onContextMenu={(e) => e.preventDefault()}
        >
          {/* 新建文件 */}
          <button
            className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
            onClick={() => {
              actions.onNewFile(getTargetDir(contextMenu.node))
              closeContextMenu()
            }}
          >
            <File className="h-3 w-3" />
            新建文件
          </button>
          {/* 新建文件夹 */}
          <button
            className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
            onClick={() => {
              actions.onNewFolder(getTargetDir(contextMenu.node))
              closeContextMenu()
            }}
          >
            <Folder className="h-3 w-3" />
            新建文件夹
          </button>
          {/* 上传文件 */}
          <button
            className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
            onClick={() => {
              actions.onUpload(getTargetDir(contextMenu.node))
              closeContextMenu()
            }}
          >
            <UploadIcon />
            上传文件
          </button>
          {/* 分隔线 */}
          {contextMenu.node && (
            <div className="my-1 border-t border-[var(--color-border-primary)]" />
          )}
          {/* 复制路径 */}
          {contextMenu.node && (
            <button
              className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
              onClick={() => {
                actions.onCopyPath(contextMenu.node!)
                closeContextMenu()
              }}
            >
              <CopyIcon />
              查看绝对路径
            </button>
          )}
          {/* 重命名 */}
          {contextMenu.node && (
            <button
              className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]"
              onClick={() => {
                actions.onRename(contextMenu.node!)
                closeContextMenu()
              }}
            >
              <RenameIcon />
              重命名
            </button>
          )}
          {/* 删除 */}
          {contextMenu.node && (
            <button
              className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-[var(--color-text-error)] hover:bg-[var(--color-surface-error)]"
              onClick={() => {
                actions.onDelete(contextMenu.node!)
                closeContextMenu()
              }}
            >
              <DeleteIcon />
              删除
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function CopyIcon() {
  return (
    <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <rect x="9" y="9" width="13" height="13" rx="2" />
      <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
    </svg>
  )
}

function UploadIcon() {
  return (
    <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4" />
      <polyline points="17 8 12 3 7 8" />
      <line x1="12" y1="3" x2="12" y2="15" />
    </svg>
  )
}

function RenameIcon() {
  return (
    <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25z" />
      <path d="M20.71 7.04c.39-.39.39-1.02 0-1.41l-2.34-2.34c-.39-.39-1.02-.39-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z" />
    </svg>
  )
}

function DeleteIcon() {
  return (
    <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" />
    </svg>
  )
}

function FileTreeNode({
  node,
  depth,
  selectedPath,
  onSelect,
  actions,
  onContextMenu,
}: {
  node: FileNode
  depth: number
  selectedPath?: string
  onSelect: (path: string) => void
  actions?: FileTreeActions
  onContextMenu: (e: React.MouseEvent, node: FileNode) => void
}) {
  const [expanded, setExpanded] = useState(depth === 0)
  const isSelected = selectedPath === node.path

  function handleClick() {
    if (node.is_dir) {
      setExpanded(!expanded)
    } else {
      onSelect(node.path)
    }
  }

  return (
    <div>
      <div
        className={cn(
          'flex items-center gap-1 cursor-pointer rounded px-2 py-1 hover:bg-[var(--color-surface-hover)] transition-colors',
          isSelected && 'bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)]',
        )}
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
        onClick={handleClick}
        onContextMenu={(e) => onContextMenu(e, node)}
      >
        {node.is_dir ? (
          <>
            {expanded ? <ChevronDown className="h-3.5 w-3.5 shrink-0" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
            {expanded ? <FolderOpen className="h-4 w-4 shrink-0 text-[var(--color-text-warning)]" /> : <Folder className="h-4 w-4 shrink-0 text-[var(--color-text-warning)]" />}
          </>
        ) : (
          <>
            <span className="w-3.5" />
            <File className="h-4 w-4 shrink-0 text-[var(--color-text-secondary)]" />
          </>
        )}
        <span className="truncate ml-1">{node.name}</span>
      </div>
      {node.is_dir && expanded && node.children && (
        <div>
          {node.children.map((child) => (
            <FileTreeNode
              key={child.path}
              node={child}
              depth={depth + 1}
              selectedPath={selectedPath}
              onSelect={onSelect}
              actions={actions}
              onContextMenu={onContextMenu}
            />
          ))}
        </div>
      )}
    </div>
  )
}
