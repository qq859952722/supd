// REQ-U-015: 编辑器标签状态管理
// 多标签持久化到localStorage、最多8个标签（REQ-2.9.9）

import { create } from 'zustand'
import { toast } from '@/components/ui/Toast'

const EDITOR_TABS_KEY = 'supd_editor_tabs'
const EDITOR_ACTIVE_KEY = 'supd_editor_active'
const MAX_TABS = 8

export interface EditorTab {
  id: string
  name: string
  path: string
  content: string
  isDirty: boolean
}

interface EditorState {
  tabs: EditorTab[]
  activeTabId: string | null

  openTab: (tab: Omit<EditorTab, 'isDirty'> & { isDirty?: boolean }) => void
  closeTab: (id: string) => void
  closeOtherTabs: (id: string) => void
  closeAllTabs: () => void
  closeTabsToRight: (id: string) => void
  setActiveTab: (id: string) => void
  updateTabContent: (id: string, content: string) => void
  markTabSaved: (id: string) => void
}

function loadTabs(): EditorTab[] {
  try {
    const stored = localStorage.getItem(EDITOR_TABS_KEY)
    if (stored) {
      const parsed = JSON.parse(stored) as EditorTab[]
      return parsed.slice(0, MAX_TABS)
    }
  } catch {
    // localStorage不可用
  }
  return []
}

function saveTabs(tabs: EditorTab[]) {
  try {
    localStorage.setItem(EDITOR_TABS_KEY, JSON.stringify(tabs.slice(0, MAX_TABS)))
  } catch {
    // localStorage不可用时静默失败
  }
}

function loadActiveTabId(): string | null {
  try {
    return localStorage.getItem(EDITOR_ACTIVE_KEY)
  } catch {
    return null
  }
}

function saveActiveTabId(id: string | null) {
  try {
    if (id) {
      localStorage.setItem(EDITOR_ACTIVE_KEY, id)
    } else {
      localStorage.removeItem(EDITOR_ACTIVE_KEY)
    }
  } catch {
    // 静默失败
  }
}

export const useEditorStore = create<EditorState>((set, get) => {
  const initialTabs = loadTabs()
  const initialActiveId = loadActiveTabId()

  return {
    tabs: initialTabs,
    activeTabId: initialActiveId && initialTabs.find((t) => t.id === initialActiveId)
      ? initialActiveId
      : initialTabs[0]?.id ?? null,

    openTab: (tab) => {
      const { tabs } = get()
      const existing = tabs.find((t) => t.id === tab.id)
      if (existing) {
        // 已存在，切换到该标签
        set({ activeTabId: tab.id })
        saveActiveTabId(tab.id)
        return
      }
      // 新增标签
      let newTabs = [...tabs]
      if (newTabs.length >= MAX_TABS) {
        // E-04-001: 达到上限时明确提示用户，不静默淘汰已有标签
        toast.warning(`标签页已达上限 ${MAX_TABS} 个，请先关闭部分标签`)
        return
      }
      newTabs.push({ ...tab, isDirty: tab.isDirty ?? false })
      set({ tabs: newTabs, activeTabId: tab.id })
      saveTabs(newTabs)
      saveActiveTabId(tab.id)
    },

    closeTab: (id) => {
      const { tabs, activeTabId } = get()
      const newTabs = tabs.filter((t) => t.id !== id)
      let newActiveId = activeTabId
      if (activeTabId === id) {
        // 关闭的是当前标签，切换到相邻标签
        const closedIdx = tabs.findIndex((t) => t.id === id)
        if (newTabs.length > 0) {
          newActiveId = newTabs[Math.min(closedIdx, newTabs.length - 1)]?.id ?? null
        } else {
          newActiveId = null
        }
      }
      set({ tabs: newTabs, activeTabId: newActiveId })
      saveTabs(newTabs)
      saveActiveTabId(newActiveId)
    },

    setActiveTab: (id) => {
      set({ activeTabId: id })
      saveActiveTabId(id)
    },

    closeOtherTabs: (id) => {
      const { tabs } = get()
      const newTabs = tabs.filter((t) => t.id === id)
      set({ tabs: newTabs, activeTabId: id })
      saveTabs(newTabs)
      saveActiveTabId(id)
    },

    closeAllTabs: () => {
      set({ tabs: [], activeTabId: null })
      saveTabs([])
      saveActiveTabId(null)
    },

    closeTabsToRight: (id) => {
      const { tabs, activeTabId } = get()
      const idx = tabs.findIndex((t) => t.id === id)
      if (idx < 0) return
      const newTabs = tabs.slice(0, idx + 1)
      let newActiveId = activeTabId
      if (activeTabId && !newTabs.find((t) => t.id === activeTabId)) {
        newActiveId = id
      }
      set({ tabs: newTabs, activeTabId: newActiveId })
      saveTabs(newTabs)
      saveActiveTabId(newActiveId)
    },

    updateTabContent: (id, content) => {
      const { tabs } = get()
      const newTabs = tabs.map((t) =>
        t.id === id ? { ...t, content, isDirty: true } : t,
      )
      set({ tabs: newTabs })
      saveTabs(newTabs)
    },

    markTabSaved: (id) => {
      const { tabs } = get()
      const newTabs = tabs.map((t) =>
        t.id === id ? { ...t, isDirty: false } : t,
      )
      set({ tabs: newTabs })
      saveTabs(newTabs)
    },
  }
})
