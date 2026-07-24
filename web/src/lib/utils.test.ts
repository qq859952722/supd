import { describe, it, expect } from 'vitest'
import { cn } from '@/lib/utils'

describe('cn', () => {
  it('合并多个字符串类名', () => {
    expect(cn('a', 'b')).toBe('a b')
  })

  it('忽略 falsy 值（false/null/undefined/空字符串）', () => {
    expect(cn('a', false, null, undefined, '', 'b')).toBe('a b')
  })

  it('支持数组与对象写法', () => {
    expect(cn(['x', 'y'], { active: true, hidden: false })).toBe('x y active')
  })

  it('使用 tailwind-merge 去重冲突类', () => {
    // px-2 与 px-4 冲突，后者覆盖前者
    expect(cn('px-2', 'px-4')).toBe('px-4')
    expect(cn('text-red-500', 'text-blue-500')).toBe('text-blue-500')
  })
})
