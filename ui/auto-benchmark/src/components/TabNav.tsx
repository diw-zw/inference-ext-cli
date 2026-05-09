import { useState } from 'react'
import { cn } from '@/lib/utils'

export interface Tab {
  id: string
  label: string
  badge?: number | string
}

interface TabNavProps {
  tabs: Tab[]
  activeId: string
  onChange: (id: string) => void
}

export function TabNav({ tabs, activeId, onChange }: TabNavProps) {
  return (
    <div className="flex border-b border-border">
      {tabs.map(tab => (
        <button
          key={tab.id}
          onClick={() => onChange(tab.id)}
          className={cn(
            "relative px-5 py-3 text-sm font-medium transition-colors",
            "hover:text-foreground",
            activeId === tab.id
              ? "text-foreground"
              : "text-muted-foreground"
          )}
        >
          {tab.label}
          {tab.badge !== undefined && (
            <span className={cn(
              "ml-2 inline-flex items-center justify-center min-w-[20px] h-5 rounded-full px-1.5 text-xs font-medium",
              activeId === tab.id
                ? "bg-primary/20 text-primary"
                : "bg-secondary text-muted-foreground"
            )}>
              {tab.badge}
            </span>
          )}
          {activeId === tab.id && (
            <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-primary rounded-full" />
          )}
        </button>
      ))}
    </div>
  )
}
