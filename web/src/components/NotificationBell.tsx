// 节点 09-3：顶栏铃铛红点。位于全局搜索与用户菜单之间，点击导航到 /notifications。

import { Bell } from 'lucide-react'
import { useNavigate } from 'react-router'
import { useNotificationChanges } from '@/hooks/useNotificationChanges'
import { t } from '@/lib/i18n'

export function NotificationBell() {
  const navigate = useNavigate()
  const { hasUnread } = useNotificationChanges()

  return (
    <button
      type="button"
      aria-label={t.nav.notifications}
      title={t.nav.notifications}
      onClick={() => navigate('/notifications')}
      className="relative rounded-md p-2 text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-secondary)] transition-colors"
    >
      <Bell className="h-4 w-4" />
      {/* 纯 CSS 红点（无数字徽标）；符合设计稿"任何未读即红点" */}
      {hasUnread && (
        <span className="absolute right-1.5 top-1.5 block h-2 w-2 rounded-full bg-[var(--color-accent-danger)] ring-2 ring-[var(--color-bg-secondary)]" />
      )}
    </button>
  )
}