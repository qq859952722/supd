import { describe, it, expect } from 'vitest'
import { t } from '@/lib/i18n'

describe('i18n 文案对象', () => {
  it('基础文案映射存在', () => {
    expect(t.app.title).toBe('supd')
    expect(t.nav.services).toBe('服务')
    expect(t.common.noData).toBe('暂无数据')
  })

  it('服务状态枚举文案完整覆盖 7 种状态', () => {
    expect(t.status.pending).toBe('等待中')
    expect(t.status.starting).toBe('启动中')
    expect(t.status.up).toBe('运行中')
    expect(t.status.ready).toBe('就绪')
    expect(t.status.stopping).toBe('停止中')
    expect(t.status.down).toBe('已停止')
    expect(t.status.failed).toBe('失败')
  })

  it('事件类型文案覆盖 14 种事件', () => {
    // 抽样核验关键事件文案
    expect(t.events.typeServiceDied).toBe('服务死亡')
    expect(t.events.typeConfigReloaded).toBe('配置重载')
    expect(t.events.typeExtTimeout).toBe('扩展超时')
  })
})
