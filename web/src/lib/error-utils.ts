/**
 * 从 unknown 错误中提取可展示的错误消息。
 * 优先使用 Error.message（ApiException 已自动本地化），
 * 回退到 fallback。
 */
export function getErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof Error && err.message) return err.message
  return fallback
}
