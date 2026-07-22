// env.yaml 解析与序列化共享工具
// 提取自 ExtensionDetail.tsx，供 ServiceDetail / service EnvEditor / settings EnvEditor 复用
// 格式规格 §2.3.3: env: { KEY: { value, enabled?, hint? } }

export interface EnvEntry {
  key: string
  value: string
  enabled: boolean
  hint: string
}

// REQ-F-015: 敏感字段启发式识别（PASSWORD/PWD/SECRET/TOKEN/KEY）
export function isSensitiveKey(key: string): boolean {
  const upper = key.toUpperCase()
  return ['PASSWORD', 'PWD', 'SECRET', 'TOKEN', 'KEY'].some((kw) => upper.includes(kw))
}

// YAML 字符串转义：含特殊字符时加双引号
export function yamlStr(v: string): string {
  if (!v) return '""'
  if (/[:#{}\[\],&*!|>'"%@`\n]/.test(v) || v.startsWith(' ') || v.endsWith(' ')) {
    return `"${v.replace(/"/g, '\\"')}"`
  }
  return v
}

// 去除 YAML 行尾注释：# 前必须是空格/行首，且不在引号内
// YAML 规范：# 只有在其前导字符是空格/Tab 或处于行首时才是注释起始
function stripYamlComment(raw: string): string {
  let inSingle = false
  let inDouble = false
  for (let idx = 0; idx < raw.length; idx++) {
    const ch = raw[idx]
    if (ch === "'" && !inDouble) inSingle = !inSingle
    else if (ch === '"' && !inSingle) inDouble = !inDouble
    else if (ch === '#' && !inSingle && !inDouble) {
      if (idx === 0 || raw[idx - 1] === ' ' || raw[idx - 1] === '\t') {
        return raw.slice(0, idx)
      }
    }
  }
  return raw
}

// 解析 env.yaml 文本为条目列表
export function parseEnvYaml(yaml: string): EnvEntry[] {
  const entries: EnvEntry[] = []
  const lines = yaml.split('\n')
  let i = 0
  let inEnv = false
  let currentKey = ''
  let current: EnvEntry | null = null
  let keyIndent = -1 // 变量名行缩进（由第一个缩进行确定，兼容 2/4 空格）

  while (i < lines.length) {
    const raw = lines[i]
    if (!raw) { i++; continue }
    // 去除行尾注释：# 前必须是空格/行首，且不在引号内（YAML 注释规则）
    const line = stripYamlComment(raw)
    const trimmed = line.trim()
    if (!trimmed) { i++; continue }

    // 顶层 env: 块开始
    if (!line.startsWith(' ') && !line.startsWith('\t')) {
      if (trimmed === 'env:') {
        inEnv = true
      } else {
        inEnv = false
      }
      i++
      continue
    }

    if (!inEnv) { i++; continue }

    // env 块内的缩进行
    const indent = line.length - line.trimStart().length
    // 兼容 2/4 空格等不同缩进：第一个缩进行确定变量名行的缩进
    // 后端 yaml.Marshal 默认 4 空格，前端 serializeEnvYaml 使用 2 空格
    if (keyIndent < 0) keyIndent = indent
    if (indent === keyIndent && trimmed.endsWith(':')) {
      // 保存上一条
      if (current) entries.push(current)
      currentKey = trimmed.slice(0, -1).trim()
      current = { key: currentKey, value: '', enabled: true, hint: '' }
    } else if (current && indent > keyIndent && trimmed.includes(':')) {
      const colonIdx = trimmed.indexOf(':')
      const subKey = trimmed.slice(0, colonIdx).trim()
      const subVal = trimmed.slice(colonIdx + 1).trim()
      // 去引号
      let v = subVal
      if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) {
        v = v.slice(1, -1)
      }
      if (subKey === 'value') current.value = v
      else if (subKey === 'enabled') current.enabled = v !== 'false'
      else if (subKey === 'hint') current.hint = v
    }
    i++
  }
  if (current) entries.push(current)
  return entries
}

// 序列化条目列表为 env.yaml 文本
export function serializeEnvYaml(entries: EnvEntry[]): string {
  if (entries.length === 0) return 'env: {}\n'
  const lines: string[] = ['env:']
  for (const e of entries) {
    if (!e.key) continue
    lines.push(`  ${e.key}:`)
    lines.push(`    value: ${yamlStr(e.value)}`)
    if (!e.enabled) lines.push(`    enabled: false`)
    if (e.hint) lines.push(`    hint: ${yamlStr(e.hint)}`)
  }
  return lines.join('\n') + '\n'
}

// --- JSON API 辅助 ---

// 后端 /api/settings/env 和 /api/services/{name}/env 的 JSON 格式
export interface EnvFileJson {
  env: Record<string, { value: string; enabled?: boolean; hint?: string }>
}

// EnvEntry[] → EnvFileJson（用于 PUT 请求体）
// enabled=true 时省略字段（与后端 omitempty + nil=true 语义一致）
export function entriesToEnvFileJson(entries: EnvEntry[]): EnvFileJson {
  const env: EnvFileJson['env'] = {}
  for (const e of entries) {
    if (!e.key) continue
    const item: { value: string; enabled?: boolean; hint?: string } = { value: e.value }
    if (!e.enabled) item.enabled = false
    if (e.hint) item.hint = e.hint
    env[e.key] = item
  }
  return { env }
}

// EnvFileJson → EnvEntry[]（用于 GET 响应解析）
// enabled 缺省时默认 true（与后端 *bool nil=true 语义一致）
export function envFileJsonToEntries(data: EnvFileJson): EnvEntry[] {
  return Object.entries(data.env ?? {}).map(([key, v]) => ({
    key,
    value: v.value ?? '',
    enabled: v.enabled !== false,
    hint: v.hint ?? '',
  }))
}
