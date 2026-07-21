// REQ-U-002: 整体布局
// 顶部7Tab导航 + 主内容区路由切换 + 底部浮窗

import { Outlet, NavLink, useNavigate } from 'react-router'
import { ThemeProvider, useTheme } from '@/lib/theme'
import { QueryClientProvider, useQuery, useQueryClient } from '@tanstack/react-query'
import { createQueryClient } from '@/lib/query-client'
import { Toaster } from 'sonner'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import { t } from '@/lib/i18n'
import { BottomDrawer } from '@/components/BottomDrawer'
import { GlobalSearch } from '@/components/GlobalSearch'
import { useAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { TaskToastProvider } from '@/components/ui/TaskToast'
import { toast } from '@/components/ui/Toast'
import { apiGet, apiPost } from '@/lib/api-client'
import {
  LayoutDashboard,
  Server,
  Puzzle,
  Clock,
  Radio,
  FolderOpen,
  Settings,
  Search,
  User,
  Moon,
  Sun,
  Monitor,
  WifiOff,
} from 'lucide-react'
import { useState, useCallback, useEffect } from 'react'

const queryClient = createQueryClient()

function NavItem({ to, icon: Icon, label }: { to: string; icon: React.ComponentType<{ className?: string }>; label: string }) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        `flex items-center gap-2 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
          isActive
            ? 'bg-[var(--color-surface-hover)] text-[var(--color-brand-primary)]'
            : 'text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-secondary)]'
        }`
      }
    >
      <Icon className="h-4 w-4" />
      <span>{label}</span>
    </NavLink>
  )
}

function ThemeToggle() {
  const { theme, toggleTheme } = useTheme()
  return (
    <button
      onClick={toggleTheme}
      className="rounded-md p-2 text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-secondary)] transition-colors"
      title={theme === 'dark' ? '切换浅色' : '切换深色'}
    >
      {theme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </button>
  )
}



// E-07-001: 扁平化文件树，提取所有文件节点（非目录）用于搜索
interface SearchFileNode {
  name: string
  path: string
  is_dir: boolean
  children?: SearchFileNode[]
}
function flattenFileTree(nodes: SearchFileNode[] | undefined, acc: { name: string; path: string }[] = []): { name: string; path: string }[] {
  if (!nodes) return acc
  for (const node of nodes) {
    if (!node.is_dir) {
      acc.push({ name: node.name, path: node.path })
    }
    if (node.children && node.children.length > 0) {
      flattenFileTree(node.children, acc)
    }
  }
  return acc
}

function AppLayout() {
  const [searchOpen, setSearchOpen] = useState(false)
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  // S-07: 接入真实数据源 — 服务列表用于全局搜索
  const { data: servicesData } = useQuery({
    queryKey: ['services-list'],
    queryFn: () => apiGet<{ services: Array<{ name: string; status: string }> }>('/api/services'),
    refetchInterval: 10_000,
  })

  // S-07: 接入真实数据源 — 扩展列表用于全局搜索
  const { data: extensionsData } = useQuery({
    queryKey: ['extensions-list'],
    queryFn: () => apiGet<Array<{ name: string; description?: string; service?: string }>>('/api/extensions'),
    refetchInterval: 10_000,
  })

  // E-07-001: 文件树用于全局搜索（§2.9.7 要求搜索文件名）
  const { data: filesTreeData } = useQuery({
    queryKey: ['files-tree-search'],
    queryFn: () => apiGet<SearchFileNode[]>('/api/files/tree'),
    refetchInterval: 60_000,
  })

  // E-07-001: 最近事件用于全局搜索（§2.9.7 要求搜索事件内容）
  const { data: eventsSearchData } = useQuery({
    queryKey: ['events-search'],
    queryFn: () => apiGet<{ data: Array<{ time: string; type: string; payload: Record<string, unknown> }> }>('/api/events', { limit: 200 }),
    refetchInterval: 30_000,
  })

  // S-07: 接入真实数据源 — 运行中任务用于底部浮窗
  const { data: runningRuns } = useQuery({
    queryKey: ['extensions-running'],
    queryFn: () => apiGet<Array<{
      run_id: string
      extension_name: string
      action_id?: string
      service_name?: string
      state: string
      progress?: number
      started_at: string
      finished_at?: string
    }>>('/api/extensions/runs?include_recent=true'),
    refetchInterval: 3_000,
  })

  // P-03-01: 全局网络状态检测 — 定期 ping /api/health，断开时在顶部显示 banner
  const { isError: isNetworkDown } = useQuery({
    queryKey: ['network-health'],
    queryFn: () => apiGet<{ status: string }>('/api/health', undefined, true),
    refetchInterval: 10_000,
    retry: 1,
  })

  // REQ-U-017: Ctrl+K打开全局搜索
  const handleCtrlK = useCallback((e: KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
      e.preventDefault()
      setSearchOpen((prev) => !prev)
    }
  }, [])

  useEffect(() => {
    window.addEventListener('keydown', handleCtrlK)
    return () => window.removeEventListener('keydown', handleCtrlK)
  }, [handleCtrlK])

  // S-07: 运行中任务数据 — 从 API 获取真实数据
  // E-08-001/F-04-001 修复：保留原始任务状态（timeout/canceled/killed），不再坍缩为 failed
  const runningTasks: Array<{
    id: string
    extensionName: string
    action: string
    startedAt: number
    progress?: number
    status: 'running' | 'success' | 'failed' | 'timeout' | 'canceled' | 'killed'
    completedAt?: number
  }> = (runningRuns ?? []).map((run) => ({
    id: run.run_id,
    extensionName: run.extension_name,
    action: run.action_id ?? '',
    startedAt: new Date(run.started_at).getTime(),
    progress: run.progress,
    status: run.state === 'running' || run.state === 'pending' ? 'running'
      : (['success', 'failed', 'timeout', 'canceled', 'killed'].includes(run.state) ? run.state as 'success' | 'failed' | 'timeout' | 'canceled' | 'killed' : 'failed'),
    completedAt: run.finished_at ? new Date(run.finished_at).getTime() : undefined,
  }))

  // E-08-001: 取消运行中的任务 — POST /api/extensions/runs/{runID}/cancel
  const handleCancelTask = useCallback(
    async (id: string) => {
      try {
        await apiPost(`/api/extensions/runs/${encodeURIComponent(id)}/cancel`, undefined, true)
        toast.success('任务已取消')
        queryClient.invalidateQueries({ queryKey: ['extensions-running'] })
      } catch {
        toast.error('取消任务失败')
      }
    },
    [queryClient],
  )

  // E-07-001: 全局搜索 — §2.9.7 支持服务名、扩展名、文件名、事件内容
  const handleSearch = useCallback((query: string): Array<{
    id: string
    type: 'service' | 'extension' | 'file' | 'event'
    name: string
    path?: string
    description?: string
  }> => {
    if (!query.trim()) return []
    const q = query.trim().toLowerCase()
    const results: Array<{ id: string; type: 'service' | 'extension' | 'file' | 'event'; name: string; path?: string; description?: string }> = []
    // 搜索服务
    for (const svc of servicesData?.services ?? []) {
      if (svc.name.toLowerCase().includes(q)) {
        results.push({ id: svc.name, type: 'service', name: svc.name, description: svc.status })
      }
    }
    // 搜索扩展
    for (const ext of extensionsData ?? []) {
      if (ext.name.toLowerCase().includes(q)) {
        results.push({
          id: ext.name,
          type: 'extension',
          name: ext.name,
          description: ext.service ? `${ext.service} 的扩展` : (ext.description ?? '全局扩展'),
        })
      }
    }
    // 搜索文件名（仅 query 长度 >= 2 时触发，避免过短关键词匹配过多）
    if (q.length >= 2) {
      const flatFiles = flattenFileTree(filesTreeData)
      for (const f of flatFiles) {
        if (f.name.toLowerCase().includes(q)) {
          results.push({ id: f.path, type: 'file', name: f.name, path: f.path, description: f.path })
        }
      }
    }
    // 搜索最近事件（匹配 type 或 payload.message/error）
    const events = eventsSearchData?.data ?? []
    for (const evt of events) {
      const msg = typeof evt.payload.message === 'string' ? evt.payload.message
        : typeof evt.payload.error === 'string' ? evt.payload.error : ''
      if (evt.type.toLowerCase().includes(q) || msg.toLowerCase().includes(q)) {
        results.push({
          id: `${evt.type}-${evt.time}`,
          type: 'event',
          name: evt.type,
          description: msg || evt.time,
        })
      }
    }
    return results
  }, [servicesData, extensionsData, filesTreeData, eventsSearchData])

  const handleSelectSearch = useCallback((item: { id: string; type: string; name: string; path?: string }) => {
    if (item.type === 'service') {
      navigate(`/services/${encodeURIComponent(item.id)}`)
    } else if (item.type === 'extension') {
      navigate(`/extensions/${encodeURIComponent(item.id)}`)
    } else if (item.type === 'file') {
      // 跳转到文件页并选中文件路径
      navigate(`/files?path=${encodeURIComponent(item.path ?? item.id)}`)
    } else if (item.type === 'event') {
      navigate('/events')
    }
  }, [navigate])

  return (
    <div className="flex h-screen flex-col overflow-hidden">
      {/* 顶部导航栏 */}
      <header className="flex items-center justify-between border-b border-[var(--color-border-primary)] bg-[var(--color-bg-secondary)] px-4 h-12 shrink-0 overflow-x-auto">
        <div className="flex items-center gap-1 min-w-0 shrink-0">
          {/* Logo + 标题 */}
          <div className="flex items-center gap-2 mr-4 shrink-0">
            <div className="flex h-7 w-7 items-center justify-center rounded-md bg-[var(--color-brand-primary)]">
              <Server className="h-4 w-4 text-[var(--color-text-inverse)]" />
            </div>
            <span className="text-sm font-semibold text-[var(--color-text-primary)]">{t.app.title}</span>
          </div>

          {/* 7个Tab导航 */}
          <nav className="flex items-center gap-1 shrink-0">
            <NavItem to="/" icon={LayoutDashboard} label={t.nav.dashboard} />
            <NavItem to="/services" icon={Server} label={t.nav.services} />
            <NavItem to="/extensions" icon={Puzzle} label={t.nav.extensions} />
            <NavItem to="/cron" icon={Clock} label={t.nav.schedules} />
            <NavItem to="/events" icon={Radio} label={t.nav.events} />
            <NavItem to="/files" icon={FolderOpen} label={t.nav.files} />
            <NavItem to="/settings" icon={Settings} label={t.nav.settings} />
          </nav>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          {/* 全局搜索按钮 */}
          <button
            onClick={() => setSearchOpen(!searchOpen)}
            className="flex items-center gap-2 rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-surface-secondary)] px-3 py-1.5 text-sm text-[var(--color-text-tertiary)] hover:border-[var(--color-border-focus)] transition-colors"
          >
            <Search className="h-3.5 w-3.5" />
            <span>{t.search.placeholder}</span>
            <kbd className="ml-4 rounded border border-[var(--color-border-primary)] px-1.5 py-0.5 text-xs">⌘K</kbd>
          </button>

          {/* 主题切换 */}
          <ThemeToggle />

          {/* 用户菜单 */}
          <button className="rounded-md p-2 text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-secondary)] transition-colors">
            <User className="h-4 w-4" />
          </button>
        </div>
      </header>

      {/* P-03-01: 全局网络断开指示器 */}
      {isNetworkDown && (
        <div className="flex items-center gap-2 border-b border-[var(--color-border-error)] bg-[var(--color-surface-error)] px-4 py-2 text-sm text-[var(--color-text-error)]">
          <WifiOff className="h-4 w-4 shrink-0" />
          <span>无法连接到 supd 服务，请检查服务是否运行</span>
        </div>
      )}

      {/* 主内容区 */}
      <main className="flex-1 overflow-auto bg-[var(--color-bg-primary)] p-4">
        <ErrorBoundary>
          <Outlet />
        </ErrorBoundary>
      </main>

      {/* REQ-U-016: 底部浮窗 — 运行中任务 */}
      <BottomDrawer tasks={runningTasks} onCancelTask={handleCancelTask} />

      {/* REQ-U-017: 全局搜索面板 */}
      <GlobalSearch open={searchOpen} onClose={() => setSearchOpen(false)} onSearch={handleSearch} onSelect={handleSelectSearch} />

      {/* Toast通知 — REQ-2.9.12: 屏幕右下角（Sonner 默认） */}
      <Toaster
        position="bottom-right"
        toastOptions={{
          style: {
            background: 'var(--color-surface-elevated)',
            color: 'var(--color-text-primary)',
            border: '1px solid var(--color-border-primary)',
          },
          // E-02-001 修复：为不同类型 toast 添加左侧色条，提升深色模式视觉区分度
          classNames: {
            success: 'border-l-4 !border-l-[var(--color-text-success)]',
            error: 'border-l-4 !border-l-[var(--color-text-error)]',
            warning: 'border-l-4 !border-l-[var(--color-text-warning)]',
            info: 'border-l-4 !border-l-[var(--color-text-info)]',
          },
        }}
      />

      {/* REQ-2.7.4: 低于 1024px 显示居中提示 */}
      <MinWidthGuard />

      {/* REQ-2.7.3: 401 时显示 Token 输入对话框 */}
      <TokenDialog />
    </div>
  )
}

// REQ-2.7.4: 最小宽度提示（不做移动端适配）
function MinWidthGuard() {
  const [tooNarrow, setTooNarrow] = useState(
    typeof window !== 'undefined' ? window.innerWidth < 1024 : false
  )
  useEffect(() => {
    const handler = () => setTooNarrow(window.innerWidth < 1024)
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [])
  if (!tooNarrow) return null
  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center bg-[var(--color-bg-primary)] p-6 text-center">
      <div className="space-y-2">
        <Monitor className="mx-auto h-12 w-12 text-[var(--color-text-tertiary)]" />
        <p className="text-base text-[var(--color-text-primary)]">请在桌面浏览器访问</p>
        <p className="text-sm text-[var(--color-text-tertiary)]">supd 需要最小宽度 1024px 的桌面环境</p>
      </div>
    </div>
  )
}

// REQ-2.7.3: 401 时显示 Token 输入对话框（token 失效或 always_token 模式未登录）
function TokenDialog() {
  const { showLoginDialog, login, setShowLoginDialog } = useAuthStore()
  const [token, setToken] = useState('')

  if (!showLoginDialog) return null

  const handleSubmit = () => {
    if (!token.trim()) return
    login(token.trim())
    setToken('')
  }

  return (
    <div className="fixed inset-0 z-[150] flex items-center justify-center">
      <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" />
      <div className="relative z-10 w-full max-w-sm rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-6 shadow-md">
        <div className="mb-4 flex items-center gap-2">
          <User className="h-5 w-5 text-[var(--color-brand-primary)]" />
          <h3 className="text-base font-semibold text-[var(--color-text-primary)]">需要认证</h3>
        </div>
        <p className="mb-4 text-sm text-[var(--color-text-secondary)]">
          supd 当前认证模式需要 Token，请输入有效的访问 Token。
        </p>
        <Input
          type="password"
          placeholder="访问 Token"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') handleSubmit()
          }}
          autoFocus
        />
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="default" onClick={() => setShowLoginDialog(false)}>取消</Button>
          <Button variant="primary" onClick={handleSubmit} disabled={!token.trim()}>登录</Button>
        </div>
      </div>
    </div>
  )
}

export function App() {
  return (
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <TaskToastProvider>
          <AppLayout />
        </TaskToastProvider>
      </QueryClientProvider>
    </ThemeProvider>
  )
}
