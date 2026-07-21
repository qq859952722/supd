// REQ-U-004: Dashboard主面板
// 6个区块：顶部状态条 / 系统资源 / 服务状态总览 / 扩展任务 / 事件流 / 快捷操作

import { StatusBar } from '@/components/dashboard/StatusBar'
import { SystemResourcesPanel } from '@/components/dashboard/SystemResources'
import { ServiceOverview } from '@/components/dashboard/ServiceOverview'
import { ExtensionTasks } from '@/components/dashboard/ExtensionTasks'
import { RecentEvents } from '@/components/dashboard/RecentEvents'
import { QuickActions } from '@/components/dashboard/QuickActions'

export function Dashboard() {
  return (
    <div className="space-y-4">
      {/* 区块1: 顶部状态条 */}
      <StatusBar />

      {/* 区块2: 系统资源（按需采集） */}
      <SystemResourcesPanel />

      {/* 区块3+6: 服务状态总览 + 快捷操作 */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <ServiceOverview />
        </div>
        <div className="space-y-4">
          <QuickActions />
        </div>
      </div>

      {/* 区块4+5: 扩展任务 + 事件流 */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ExtensionTasks />
        <RecentEvents />
      </div>
    </div>
  )
}
