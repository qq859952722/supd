import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiPost } from '@/lib/api-client'
import { toast } from '@/components/ui/Toast'
import { getErrorMessage } from '@/lib/error-utils'

// I-03-001 修复：提取 ServiceCard/ServiceTable 共用的 5 个服务操作 mutation，
// 消除原本 30 行 428 tokens 的完全重复代码。
// 保持原有行为：silent=true 避免与 onError 重复 toast；提取后端错误消息。
export function useServiceActions(name: string) {
  const queryClient = useQueryClient()

  const startMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(name)}/start`, undefined, true),
    onSuccess: () => { toast.success(`${name} 启动指令已发送`); queryClient.invalidateQueries({ queryKey: ['services-list'] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '启动失败')) },
  })

  const stopMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(name)}/stop`, undefined, true),
    onSuccess: () => { toast.success(`${name} 停止指令已发送`); queryClient.invalidateQueries({ queryKey: ['services-list'] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '停止失败')) },
  })

  const restartMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(name)}/restart`, undefined, true),
    onSuccess: () => { toast.success(`${name} 重启指令已发送`); queryClient.invalidateQueries({ queryKey: ['services-list'] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '重启失败')) },
  })

  // F-03-001: failed 状态的清除失败按钮
  const clearFailedMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(name)}/clear-failed`, undefined, true),
    onSuccess: () => { toast.success(`${name} 失败状态已清除`); queryClient.invalidateQueries({ queryKey: ['services-list'] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '清除失败')) },
  })

  // F-03-002: stopping 状态卡住时的强制停止按钮
  const forceStopMutation = useMutation({
    mutationFn: () => apiPost(`/api/services/${encodeURIComponent(name)}/force-stop`, undefined, true),
    onSuccess: () => { toast.success(`${name} 强制停止指令已发送`); queryClient.invalidateQueries({ queryKey: ['services-list'] }) },
    onError: (err: unknown) => { toast.error(getErrorMessage(err, '强制停止失败')) },
  })

  return { startMutation, stopMutation, restartMutation, clearFailedMutation, forceStopMutation }
}
