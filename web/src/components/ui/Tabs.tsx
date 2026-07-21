// REQ-U-012: Tabs标签页组件

import { createContext, useContext, useState, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface TabsContextValue {
  activeTab: string
  setActiveTab: (value: string) => void
}

const TabsContext = createContext<TabsContextValue | null>(null)

function useTabs(): TabsContextValue {
  const ctx = useContext(TabsContext)
  if (!ctx) throw new Error('Tabs components must be used within Tabs')
  return ctx
}

function Tabs({ defaultValue, value, onValueChange, children }: {
  defaultValue?: string
  value?: string
  onValueChange?: (v: string) => void
  children: ReactNode
}) {
  const [internalValue, setInternalValue] = useState(defaultValue ?? '')
  const activeTab = value ?? internalValue

  return (
    <TabsContext.Provider
      value={{
        activeTab,
        setActiveTab: (v) => {
          setInternalValue(v)
          onValueChange?.(v)
        },
      }}
    >
      {children}
    </TabsContext.Provider>
  )
}

function TabsList({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div
      role="tablist"
      className={cn(
        'inline-flex h-9 items-center justify-center rounded-lg bg-[var(--color-bg-tertiary)] p-1',
        'text-[var(--color-text-secondary)]',
        className,
      )}
    >
      {children}
    </div>
  )
}

function TabsTrigger({ value, className, children, disabled }: {
  value: string
  className?: string
  disabled?: boolean
  children: ReactNode
}) {
  const { activeTab, setActiveTab } = useTabs()
  const isActive = activeTab === value

  return (
    <button
      role="tab"
      aria-selected={isActive}
      disabled={disabled}
      onClick={() => setActiveTab(value)}
      className={cn(
        'inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium',
        'transition-all duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-border-focus)] focus-visible:ring-offset-2',
        'disabled:pointer-events-none disabled:opacity-50',
        isActive
          ? 'bg-[var(--color-surface-primary)] text-[var(--color-text-primary)] shadow-sm'
          : 'text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] hover:bg-[var(--color-surface-hover)]',
        className,
      )}
    >
      {children}
    </button>
  )
}

function TabsContent({ value, className, children }: {
  value: string
  className?: string
  children: ReactNode
}) {
  const { activeTab } = useTabs()

  if (activeTab !== value) return null

  return (
    <div
      role="tabpanel"
      className={cn('mt-3 focus-visible:outline-none', className)}
    >
      {children}
    </div>
  )
}

export { Tabs, TabsList, TabsTrigger, TabsContent }
