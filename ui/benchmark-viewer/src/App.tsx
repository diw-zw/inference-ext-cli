import { useState, useMemo, useEffect } from 'react'
import type { ResultDetail } from './types'
import { TabNav } from './components/TabNav'
import { OverviewTab } from './components/OverviewTab'
import { TrialsTab } from './components/TrialsTab'
import { ConfigView } from './components/ConfigView'
import { YamlView } from './components/YamlView'
import { formatDate } from './lib/utils'
import { Zap, TrendingUp, Loader2, AlertCircle } from 'lucide-react'

function App() {
  const [data, setData] = useState<ResultDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [activeTab, setActiveTab] = useState('overview')
  // -1 = All templates, 0..n-1 = specific template
  const [templateIdx, setTemplateIdx] = useState(-1)

  useEffect(() => {
    fetch('/data/result.json')
      .then(res => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json()
      })
      .then((json: ResultDetail) => setData(json))
      .catch(err => setError(`Failed to load result.json: ${err.message}`))
  }, [])

  const isMulti = templateIdx === -1

  const tabs = useMemo(() => [
    { id: 'overview', label: 'Overview' },
    { id: 'trials', label: 'Trials', badge: isMulti
      ? data?.status.totalTrials
      : data?.templates[templateIdx]?.trials?.length
    },
    { id: 'config', label: 'Config' },
    { id: 'yaml', label: 'Raw Data' },
  ], [data, templateIdx, isMulti])

  // Loading / error states
  if (error) {
    return (
      <div className="min-h-screen bg-background flex items-center justify-center">
        <div className="text-center space-y-3">
          <AlertCircle className="h-8 w-8 text-destructive mx-auto" />
          <p className="text-destructive font-medium">Error</p>
          <p className="text-sm text-muted-foreground">{error}</p>
        </div>
      </div>
    )
  }

  if (!data) {
    return (
      <div className="min-h-screen bg-background flex items-center justify-center">
        <div className="text-center space-y-3">
          <Loader2 className="h-8 w-8 text-primary mx-auto animate-spin" />
          <p className="text-muted-foreground text-sm">Loading result.json...</p>
        </div>
      </div>
    )
  }

  const isRunning = !data.status.endTime

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="border-b border-border bg-card/50 backdrop-blur-sm sticky top-0 z-40">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-2">
                <TrendingUp className="h-5 w-5 text-primary" />
                <h1 className="text-lg font-semibold">Auto-Benchmark Results</h1>
              </div>
              {isRunning ? (
                <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-warning/10 text-warning border border-warning/20">
                  <span className="animate-pulse h-1.5 w-1.5 rounded-full bg-warning" />
                  Running
                </span>
              ) : (
                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-success/10 text-success border border-success/20">
                  Completed
                </span>
              )}
            </div>
            <div className="text-xs text-muted-foreground">
              <span className="mr-2">{data.experimentId}</span>
              {data.status.startTime && <span>Started: {formatDate(data.status.startTime)}</span>}
            </div>
          </div>

          {/* Config strip */}
          <div className="flex flex-wrap items-center gap-x-6 gap-y-1 mt-3 text-xs text-muted-foreground">
            <span className="flex items-center gap-1">
              <Zap className="h-3 w-3" />
              {data.config.backend}
            </span>
            <span>{data.config.algorithm}</span>
            <span>optimize: {data.config.optimize}</span>
            {data.config.sla.ttftP99MaxMs && <span>TTFT P99 ≤ {data.config.sla.ttftP99MaxMs}ms</span>}
            {data.config.sla.tpotP99MaxMs && <span>TPOT P99 ≤ {data.config.sla.tpotP99MaxMs}ms</span>}
          </div>

          {/* Template tabs */}
          <div className="flex gap-1 mt-3">
            <button
              onClick={() => setTemplateIdx(-1)}
              className={`px-3 py-1 text-xs rounded-md transition-colors ${
                isMulti
                  ? 'bg-primary/10 text-primary font-medium'
                  : 'hover:bg-accent text-muted-foreground'
              }`}
            >
              All Templates
            </button>
            {data.templates.map((t, i) => (
              <button
                key={t.name}
                onClick={() => setTemplateIdx(i)}
                className={`px-3 py-1 text-xs rounded-md transition-colors ${
                  !isMulti && i === templateIdx
                    ? 'bg-primary/10 text-primary font-medium'
                    : 'hover:bg-accent text-muted-foreground'
                }`}
              >
                {t.name}
              </button>
            ))}
          </div>
        </div>
      </header>

      {/* Content */}
      <main className="max-w-7xl mx-auto px-4 sm:px-6 py-6">
        <TabNav tabs={tabs} activeId={activeTab} onChange={setActiveTab} />

        <div className="mt-6">
          {activeTab === 'overview' && (
            <OverviewTab
              data={data}
              templateIndex={isMulti ? 0 : templateIdx}
              multiTemplate={isMulti}
            />
          )}
          {activeTab === 'trials' && (
            <TrialsTab
              trials={data.templates[isMulti ? 0 : templateIdx]?.trials || []}
              optimize={data.config.optimize}
              multiTemplate={isMulti}
              allTemplates={isMulti ? data.templates : undefined}
            />
          )}
          {activeTab === 'config' && <ConfigView config={data.config} />}
          {activeTab === 'yaml' && <YamlView data={data} />}
        </div>
      </main>
    </div>
  )
}

export default App
