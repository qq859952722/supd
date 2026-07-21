// REQ-U-004: 快捷操作 — 启动全部/停止全部（需确认）/重扫配置
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiPost } from '@/lib/api-client'
import { toast } from '@/components/ui/Toast'
import { getErrorMessage } from '@/lib/error-utils'
import { t } from '@/lib/i18n'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Play, Square, RefreshCw } from 'lucide-react'
import { useState } from 'react'

export function QuickActions() {
  const queryClient = useQueryClient()
  const [showStopConfirm, setShowStopConfirm] = useState(false)

  // E-02-003 修复：silent=true 避免与 onError 重复 toast，提取后端错误消息
  const startAllMutation = useMutation({
    mutationFn: () => apiPost('/api/services/start', undefined, true),
    onSuccess: () => {
      toast.success('已发送启动全部指令')
      queryClient.invalidateQueries({ queryKey: ['services-list'] })
    },
    onError: (err: unknown) => {
      toast.error(getErrorMessage(err, '启动全部失败'))
    },
  })

  const stopAllMutation = useMutation({
    mutationFn: () => apiPost('/api/services/stop', undefined, true),
    onSuccess: () => {
      toast.success('已发送停止全部指令')
      queryClient.invalidateQueries({ queryKey: ['services-list'] })
      setShowStopConfirm(false)
    },
    onError: (err: unknown) => {
      toast.error(getErrorMessage(err, '停止全部失败'))
    },
  })

  // E-09-004: 重扫配置 — 调用 POST /api/reload 手动触发配置热重载
  const rescanMutation = useMutation({
    mutationFn: () => apiPost('/api/reload', undefined, true),
    onSuccess: () => {
      toast.success('配置已重扫')
      queryClient.invalidateQueries({ queryKey: ['services-list'] })
    },
    onError: (err: unknown) => {
      toast.error(getErrorMessage(err, '重扫配置失败'))
    },
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t.dashboard.quickActions}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex flex-wrap gap-3">
          <Button
            variant="primary"
            onClick={() => startAllMutation.mutate()}
            disabled={startAllMutation.isPending}
          >
            <Play className="h-4 w-4" />
            {t.dashboard.startAll}
          </Button>
          <Button
            variant="danger"
            onClick={() => setShowStopConfirm(true)}
            disabled={stopAllMutation.isPending}
          >
            <Square className="h-4 w-4" />
            {t.dashboard.stopAll}
          </Button>
          <Button
            variant="default"
            onClick={() => rescanMutation.mutate()}
            disabled={rescanMutation.isPending}
          >
            <RefreshCw className={rescanMutation.isPending ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} />
            {t.dashboard.rescanConfig}
          </Button>
        </div>

        {/* REQ-2.9.3: 停止全部需确认弹窗（点击遮罩不关闭） */}
        {showStopConfirm && (
          <div className="fixed inset-0 z-50 flex items-center justify-center">
            <div className="fixed inset-0 bg-black/60 backdrop-blur-sm" />
            <div className="relative z-10 w-full max-w-sm rounded-lg border border-[var(--color-border-primary)] bg-[var(--color-surface-secondary)] p-6 shadow-md">
              <h3 className="text-base font-semibold text-[var(--color-text-primary)]">确认停止全部服务</h3>
              <p className="mt-2 text-sm text-[var(--color-text-secondary)]">
                此操作将停止所有运行中的服务，可能影响正在执行的任务。确定继续吗？
              </p>
              <div className="mt-4 flex justify-end gap-2">
                <Button variant="default" onClick={() => setShowStopConfirm(false)}>
                  {t.common.cancel}
                </Button>
                <Button
                  variant="danger"
                  onClick={() => stopAllMutation.mutate()}
                  disabled={stopAllMutation.isPending}
                >
                  {t.service.stopAll}
                </Button>
              </div>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
