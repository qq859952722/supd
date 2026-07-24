import { describe, it, expect } from 'vitest'
import {
  isSensitiveKey,
  yamlStr,
  parseEnvYaml,
  serializeEnvYaml,
  entriesToEnvFileJson,
  envFileJsonToEntries,
  type EnvEntry,
} from './env-yaml'

describe('isSensitiveKey', () => {
  it('识别敏感关键字', () => {
    expect(isSensitiveKey('DB_PASSWORD')).toBe(true)
    expect(isSensitiveKey('api_key')).toBe(true) // 大小写不敏感
    expect(isSensitiveKey('SECRET_TOKEN')).toBe(true)
    expect(isSensitiveKey('MY_PWD')).toBe(true)
  })

  it('非敏感关键字返回 false', () => {
    expect(isSensitiveKey('USERNAME')).toBe(false)
    expect(isSensitiveKey('HOST')).toBe(false)
    expect(isSensitiveKey('PORT')).toBe(false)
  })
})

describe('yamlStr', () => {
  it('空值序列化为引号空串', () => {
    expect(yamlStr('')).toBe('""')
  })

  it('普通值原样输出', () => {
    expect(yamlStr('hello')).toBe('hello')
    expect(yamlStr('v123')).toBe('v123')
  })

  it('含特殊字符加双引号转义', () => {
    expect(yamlStr('a:b')).toBe('"a:b"') // 冒号
    expect(yamlStr('has # comment')).toBe('"has # comment"') // 井号
    expect(yamlStr(' leading')).toBe('" leading"') // 前导空格
    expect(yamlStr('quote"inside')).toBe('"quote\\"inside"') // 内部引号转义
  })
})

describe('parseEnvYaml', () => {
  it('解析 4 空格缩进（后端 yaml.Marshal 风格）', () => {
    const yaml = `env:
    FOO:
        value: bar
        enabled: false
    BAZ:
        value: qux
`
    const entries = parseEnvYaml(yaml)
    expect(entries).toHaveLength(2)
    expect(entries[0]).toMatchObject({ key: 'FOO', value: 'bar', enabled: false, hint: '' })
    expect(entries[1]).toMatchObject({ key: 'BAZ', value: 'qux', enabled: true, hint: '' })
  })

  it('解析 2 空格缩进（前端 serializeEnvYaml 风格）', () => {
    const yaml = `env:
  FOO:
    value: bar
  BAZ:
    value: qux
    hint: 说明
`
    const entries = parseEnvYaml(yaml)
    expect(entries).toHaveLength(2)
    expect(entries[0]).toMatchObject({ key: 'FOO', value: 'bar', enabled: true })
    expect(entries[1]).toMatchObject({ key: 'BAZ', value: 'qux', hint: '说明' })
  })

  it('去除行尾注释（# 前为空格）', () => {
    const yaml = `env:
  FOO:
    value: bar # 这是注释
`
    const entries = parseEnvYaml(yaml)
    expect(entries[0].value).toBe('bar')
  })

  it('空 env 块返回空数组', () => {
    expect(parseEnvYaml('env:\n')).toEqual([])
    expect(parseEnvYaml('env: {}\n')).toEqual([])
  })

  it('忽略非 env 顶层块', () => {
    const yaml = `services:
  web:
    value: x
env:
  FOO:
    value: bar
`
    const entries = parseEnvYaml(yaml)
    expect(entries).toHaveLength(1)
    expect(entries[0].key).toBe('FOO')
  })
})

describe('serializeEnvYaml / parseEnvYaml 往返', () => {
  it('序列化后解析保持一致（enabled=true 省略）', () => {
    const entries: EnvEntry[] = [
      { key: 'FOO', value: 'bar', enabled: true, hint: '' },
      { key: 'BAZ', value: 'qux', enabled: false, hint: '说明' },
    ]
    const yaml = serializeEnvYaml(entries)
    const back = parseEnvYaml(yaml)
    expect(back).toHaveLength(2)
    expect(back[0]).toMatchObject({ key: 'FOO', value: 'bar', enabled: true })
    expect(back[1]).toMatchObject({ key: 'BAZ', value: 'qux', enabled: false, hint: '说明' })
  })

  it('空 entries 序列化返回 env: {}\\n', () => {
    expect(serializeEnvYaml([])).toBe('env: {}\n')
  })
})

describe('entriesToEnvFileJson / envFileJsonToEntries 往返', () => {
  it('enabled=true 省略字段，往返后默认 true', () => {
    const entries: EnvEntry[] = [
      { key: 'FOO', value: 'bar', enabled: true, hint: '' },
      { key: 'BAZ', value: 'qux', enabled: false, hint: 'h' },
    ]
    const json = entriesToEnvFileJson(entries)
    expect(json.env.FOO).toEqual({ value: 'bar' }) // 无 enabled/hint
    expect(json.env.BAZ).toEqual({ value: 'qux', enabled: false, hint: 'h' })

    const back = envFileJsonToEntries(json)
    expect(back[0]).toMatchObject({ key: 'FOO', value: 'bar', enabled: true })
    expect(back[1]).toMatchObject({ key: 'BAZ', value: 'qux', enabled: false, hint: 'h' })
  })

  it('缺失的 enabled 字段默认 true', () => {
    const data = { env: { FOO: { value: 'bar' } } }
    const back = envFileJsonToEntries(data)
    expect(back[0]).toMatchObject({ key: 'FOO', value: 'bar', enabled: true })
  })
})
