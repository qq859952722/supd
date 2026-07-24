import { describe, it, expect } from 'vitest'
import { getErrorMessage } from './error-utils'

describe('getErrorMessage', () => {
  it('返回 Error.message（ApiException 已本地化）', () => {
    expect(getErrorMessage(new Error('服务不存在'), '默认')).toBe('服务不存在')
  })

  it('Error 但 message 为空时回退到 fallback', () => {
    expect(getErrorMessage(new Error(''), '默认提示')).toBe('默认提示')
  })

  it('非 Error（字符串/数字/undefined）回退到 fallback', () => {
    expect(getErrorMessage('plain string', '默认')).toBe('默认')
    expect(getErrorMessage(123, '默认')).toBe('默认')
    expect(getErrorMessage(undefined, '默认')).toBe('默认')
    expect(getErrorMessage(null, '默认')).toBe('默认')
  })
})
