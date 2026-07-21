// REQ-U-015: Monaco YAML编辑器封装
// monaco-yaml语言服务集成、多语言高亮、查找替换、格式化、Ctrl+S保存、编辑器工具栏

import { useRef, useCallback, useEffect, useState, type CSSProperties } from 'react'
import Editor, { type OnMount, type Monaco } from '@monaco-editor/react'
import { configureMonacoYaml } from 'monaco-yaml'
import { Save, Wand2, Search, Replace, Code } from 'lucide-react'
import { useTheme } from '@/lib/theme'
import { Select } from '@/components/ui/Select'

let yamlConfigured = false

function ensureYamlSetup(monaco: Monaco) {
  if (yamlConfigured) return
  yamlConfigured = true
  configureMonacoYaml(monaco, {
    enableSchemaRequest: false,
    schemas: [],
  })
}

// 按文件扩展名自动检测Monaco语言ID
function detectLanguage(filename?: string): string {
  if (!filename) return 'yaml'
  const lower = filename.toLowerCase()
  const dotIdx = lower.lastIndexOf('.')
  const ext = dotIdx >= 0 ? lower.slice(dotIdx + 1) : lower
  switch (ext) {
    case 'yaml':
    case 'yml':
      return 'yaml'
    case 'json':
      return 'json'
    case 'sh':
    case 'bash':
    case 'zsh':
      return 'shell'
    case 'py':
      return 'python'
    case 'js':
    case 'jsx':
    case 'mjs':
    case 'cjs':
      return 'javascript'
    case 'ts':
    case 'tsx':
      return 'typescript'
    case 'md':
    case 'markdown':
      return 'markdown'
    case 'ini':
    case 'conf':
    case 'cfg':
    case 'properties':
      return 'ini'
    case 'sql':
      return 'sql'
    default:
      return 'plaintext'
  }
}

// 支持的语言列表（用于下拉切换）
const LANGUAGE_OPTIONS: { value: string; label: string }[] = [
  { value: 'yaml', label: 'YAML' },
  { value: 'json', label: 'JSON' },
  { value: 'shell', label: 'Shell' },
  { value: 'python', label: 'Python' },
  { value: 'javascript', label: 'JavaScript' },
  { value: 'typescript', label: 'TypeScript' },
  { value: 'markdown', label: 'Markdown' },
  { value: 'ini', label: 'INI' },
  { value: 'sql', label: 'SQL' },
  { value: 'plaintext', label: '纯文本' },
]

// 简单JSON格式化：解析后2空格缩进重新输出
function formatJson(text: string): string {
  return JSON.stringify(JSON.parse(text), null, 2)
}

// 简单YAML格式化：去除行尾空白、统一缩进（tab→2空格）、规范键值分隔符为单个空格
function formatYaml(text: string): string {
  return text
    .split('\n')
    .map((line) => {
      const trimmed = line.trim()
      // 空行或纯注释行只trim尾空白
      if (trimmed === '' || trimmed.startsWith('#')) {
        return trimmed
      }
      // 提取缩进：tab统一转为2空格
      const indentMatch = line.match(/^[ \t]*/)
      const rawIndent = indentMatch ? indentMatch[0] : ''
      const indent = rawIndent.replace(/\t/g, '  ')
      let content = line.slice(rawIndent.length).trimEnd()
      // 规范键值分隔符：`key:value` → `key: value`、`key:  value` → `key: value`
      // 仅对非列表项、非引号开头的简单键值行生效
      if (!content.startsWith('-') && !content.startsWith('"') && !content.startsWith("'")) {
        content = content.replace(/^([^\s#:][^\s#]*?):\s*/, '$1: ')
      }
      return indent + content
    })
    .join('\n')
}

interface MonacoEditorProps {
  value: string
  onChange?: (value: string) => void
  onSave?: (value: string) => void
  readOnly?: boolean
  filename?: string
  height?: string | number
}

const TOOLBAR_BTN_CLASS =
  'inline-flex h-7 w-7 items-center justify-center rounded text-[var(--color-text-secondary)] hover:bg-[var(--color-surface-hover)] hover:text-[var(--color-text-primary)] disabled:pointer-events-none disabled:opacity-40 transition-colors'

export function MonacoEditor({
  value,
  onChange,
  onSave,
  readOnly = false,
  filename,
  height = '100%',
}: MonacoEditorProps) {
  const editorRef = useRef<Parameters<OnMount>[0] | null>(null)
  const monacoRef = useRef<Monaco | null>(null)
  const [language, setLanguage] = useState<string>(() => detectLanguage(filename))
  const [formatError, setFormatError] = useState<string | null>(null)
  // E-04-001 修复：Monaco 主题跟随应用主题切换
  const { theme } = useTheme()
  const monacoTheme = theme === 'dark' ? 'vs-dark' : 'light'

  // 文件名变化时重新自动检测语言
  useEffect(() => {
    setLanguage(detectLanguage(filename))
  }, [filename])

  // 语言变化时同步到Monaco model（保证下拉切换立即生效）
  useEffect(() => {
    const editor = editorRef.current
    const monaco = monacoRef.current
    if (!editor || !monaco) return
    const model = editor.getModel()
    if (model) {
      monaco.editor.setModelLanguage(model, language)
    }
  }, [language])

  const handleMount: OnMount = useCallback((editor, monaco) => {
    editorRef.current = editor
    monacoRef.current = monaco
    ensureYamlSetup(monaco)

    // Ctrl+S / Cmd+S 保存
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
      if (onSave) {
        onSave(editor.getValue())
      }
    })

    // E-04 修复：拦截 Ctrl+K — Monaco 默认会捕获该组合键（用于 chord 快捷键），
    // 阻止冒泡到 window，导致 App.tsx 的全局搜索监听器无法触发。
    // 此处注册命令覆盖默认行为，并派发合成 keydown 事件转发给全局监听器。
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyK, () => {
      window.dispatchEvent(new KeyboardEvent('keydown', {
        key: 'k',
        ctrlKey: true,
        metaKey: true,
        bubbles: true,
      }))
    })

    // 设置编辑器选项：启用查找替换、格式化、多光标
    editor.updateOptions({
      fontSize: 13,
      fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      wordWrap: 'on',
      tabSize: 2,
      renderWhitespace: 'selection',
      lineNumbers: 'on',
      folding: true,
      readOnly,
      multiCursorModifier: 'ctrlCmd',
      formatOnPaste: true,
      formatOnType: true,
      find: {
        addExtraSpaceOnTop: false,
        autoFindInSelection: 'never',
        seedSearchStringFromSelection: 'always',
      },
    })
  }, [onSave, readOnly])

  // readOnly变化时更新选项
  useEffect(() => {
    if (editorRef.current) {
      editorRef.current.updateOptions({ readOnly })
    }
  }, [readOnly])

  const handleChange = useCallback(
    (newValue: string | undefined) => {
      if (onChange && newValue !== undefined) {
        onChange(newValue)
      }
    },
    [onChange],
  )

  // 工具栏动作：保存
  const handleSaveClick = useCallback(() => {
    if (onSave && editorRef.current) {
      onSave(editorRef.current.getValue())
    }
  }, [onSave])

  // 工具栏动作：格式化（JSON用解析、YAML用简单缩进对齐、其他调用Monaco内置格式化）
  const handleFormatClick = useCallback(() => {
    const editor = editorRef.current
    if (!editor) return
    const model = editor.getModel()
    try {
      if (language === 'json' && model) {
        const formatted = formatJson(editor.getValue())
        editor.executeEdits('monaco-editor.format', [
          { range: model.getFullModelRange(), text: formatted },
        ])
        setFormatError(null)
      } else if (language === 'yaml' && model) {
        const formatted = formatYaml(editor.getValue())
        editor.executeEdits('monaco-editor.format', [
          { range: model.getFullModelRange(), text: formatted },
        ])
        setFormatError(null)
      } else {
        // 调用Monaco内置格式化动作
        const action = editor.getAction('editor.action.formatDocument')
        if (action) {
          void action.run()
        }
        setFormatError(null)
      }
    } catch (err) {
      setFormatError(err instanceof Error ? err.message : String(err))
    }
  }, [language])

  // 工具栏动作：查找
  const handleFindClick = useCallback(() => {
    const editor = editorRef.current
    if (!editor) return
    const action = editor.getAction('actions.find')
    if (action) {
      void action.run()
    }
  }, [])

  // 工具栏动作：替换
  const handleReplaceClick = useCallback(() => {
    const editor = editorRef.current
    if (!editor) return
    const action = editor.getAction('editor.action.startFindReplaceAction')
    if (action) {
      void action.run()
    }
  }, [])

  const outerStyle: CSSProperties = {
    height: typeof height === 'number' ? `${height}px` : height,
  }

  return (
    <div
      className="flex w-full flex-col overflow-hidden"
      style={outerStyle}
      data-filename={filename}
    >
      {/* 编辑器工具栏 */}
      <div className="flex shrink-0 items-center gap-1 border-b border-[var(--color-border-primary)] bg-[var(--color-bg-secondary)] px-2 py-1">
        <button
          type="button"
          onClick={handleSaveClick}
          disabled={readOnly || !onSave}
          title="保存 (Ctrl+S)"
          className={TOOLBAR_BTN_CLASS}
        >
          <Save className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onClick={handleFormatClick}
          disabled={readOnly}
          title="格式化文档"
          className={TOOLBAR_BTN_CLASS}
        >
          <Wand2 className="h-3.5 w-3.5" />
        </button>
        <div className="mx-1 h-4 w-px bg-[var(--color-border-primary)]" />
        <button
          type="button"
          onClick={handleFindClick}
          title="查找 (Ctrl+F)"
          className={TOOLBAR_BTN_CLASS}
        >
          <Search className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onClick={handleReplaceClick}
          disabled={readOnly}
          title="替换 (Ctrl+H)"
          className={TOOLBAR_BTN_CLASS}
        >
          <Replace className="h-3.5 w-3.5" />
        </button>
        <div className="mx-1 h-4 w-px bg-[var(--color-border-primary)]" />
        {/* 语言模式选择 */}
        <div className="flex items-center gap-1">
          <Code className="h-3.5 w-3.5 text-[var(--color-text-tertiary)]" />
          <Select
            className="w-28"
            value={language}
            onChange={(e) => setLanguage(e.target.value)}
            options={LANGUAGE_OPTIONS}
          />
        </div>
        {formatError && (
          <span
            className="ml-auto max-w-[200px] truncate text-xs text-[var(--color-text-error)]"
            title={formatError}
          >
            格式化失败
          </span>
        )}
      </div>

      {/* 编辑器区域 */}
      <div className="min-h-0 flex-1">
        <Editor
          height="100%"
          language={language}
          value={value}
          onChange={handleChange}
          onMount={handleMount}
          theme={monacoTheme}
          options={{
            readOnly,
            automaticLayout: true,
          }}
          path={filename}
        />
      </div>
    </div>
  )
}
