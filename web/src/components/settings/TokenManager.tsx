// REQ-U-011: Token管理组件
// 验证token，重新生成需通过CLI

import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { apiPost } from '@/lib/api-client'
import { toast } from '@/components/ui/Toast'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'
import { t } from '@/lib/i18n'

interface TokenManagerProps {
  /** 是否已配置 token（来自 GET /api/settings 的 auth_token_configured 字段） */
  tokenConfigured?: boolean
  /** 当前认证模式 */
  authMode?: string
}

export function TokenManager({ tokenConfigured, authMode }: TokenManagerProps) {
  const [verifyInput, setVerifyInput] = useState('')
  const [verifyResult, setVerifyResult] = useState<boolean | null>(null)

  const verifyMutation = useMutation({
    mutationFn: (token: string) => apiPost<{ valid: boolean }>('/api/auth/verify', { token }, true),
    onSuccess: (data) => {
      setVerifyResult(data.valid)
    },
    onError: () => {
      // E-02-001: 验证失败时重置结果并提示用户
      setVerifyResult(null)
      toast.error('Token 验证请求失败')
    },
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t.settings.tokenTitle}</CardTitle>
        <CardDescription>{t.settings.tokenDesc}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* 当前 token 状态 */}
        <div className="flex items-center gap-2 text-sm">
          <span className="text-[var(--color-text-secondary)]">当前状态：</span>
          {authMode === 'none' ? (
            <Badge variant="default">未启用认证</Badge>
          ) : tokenConfigured ? (
            <Badge variant="success">已配置 Token</Badge>
          ) : (
            <Badge variant="danger">未配置 Token</Badge>
          )}
        </div>

        {/* 重新生成提示 */}
        <div className="rounded-md border border-[var(--color-border-secondary)] bg-[var(--color-bg-tertiary)] px-3 py-2 text-sm text-[var(--color-text-secondary)]">
          Token 重新生成请使用 CLI 命令：<code className="rounded bg-[var(--color-bg-secondary)] px-1 py-0.5 font-mono text-xs">supd token regenerate</code>
        </div>

        {/* 验证Token */}
        <div className="space-y-2">
          <label className="text-sm text-[var(--color-text-secondary)]">{t.settings.verifyToken}</label>
          <div className="flex gap-2">
            <Input
              value={verifyInput}
              onChange={(e) => { setVerifyInput(e.target.value); setVerifyResult(null) }}
              placeholder={t.settings.enterToken}
            />
            <Button
              variant="default"
              size="sm"
              onClick={() => verifyMutation.mutate(verifyInput)}
              disabled={!verifyInput}
            >
              {t.settings.verify}
            </Button>
          </div>
          {verifyResult !== null && (
            <p className={`text-xs ${verifyResult ? 'text-[var(--color-text-success)]' : 'text-[var(--color-text-error)]'}`}>
              {verifyResult ? t.settings.tokenValid : t.settings.tokenInvalid}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
