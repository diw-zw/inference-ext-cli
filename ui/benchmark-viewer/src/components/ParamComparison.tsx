import { useMemo } from 'react'
import { Bar } from 'react-chartjs-2'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend,
} from 'chart.js'
import type { TrialResult, ParamSet } from '@/types'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { formatNumber, computeScoreRatio, scoreColorToHsl } from '@/lib/utils'

ChartJS.register(CategoryScale, LinearScale, BarElement, Title, Tooltip, Legend)

interface ParamComparisonProps {
  trials: TrialResult[]
  optimize: string
}

function paramNameLabel(name: string): string {
  return name.replace(/([A-Z])/g, ' $1').replace(/^./, s => s.toUpperCase()).trim()
}

export function ParamComparison({ trials, optimize }: ParamComparisonProps) {
  const allParams = useMemo(() => {
    const names = new Set<string>()
    trials.forEach(t => {
      const p = t.params.default || {}
      Object.keys(p).forEach(k => names.add(k))
    })
    return Array.from(names)
  }, [trials])

  const scores = useMemo(() => trials.map(t => t.score), [trials])

  const colorMap = useMemo(() => {
    return trials.map(t => {
      const ratio = computeScoreRatio(scores, t.score, optimize)
      return scoreColorToHsl(ratio, t.slaPass)
    })
  }, [trials, scores, optimize])

  const paramNames = allParams
  if (paramNames.length === 0) {
    return (
      <Card>
        <CardHeader><CardTitle>Parameter Comparison</CardTitle></CardHeader>
        <CardContent><p className="text-muted-foreground text-sm">No parameters to compare.</p></CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Parameter Impact</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4" style={{ gridTemplateColumns: `repeat(${Math.min(paramNames.length, 2)}, 1fr)` }}>
          {paramNames.map(name => {
            const chartData = {
              labels: trials.map(t => `T${t.trialIndex}`),
              datasets: [{
                label: paramNameLabel(name),
                data: trials.map(t => {
                  const p = t.params.default || {}
                  return typeof p[name] === 'number' ? p[name] as number : 0
                }),
                backgroundColor: colorMap,
                borderColor: colorMap,
                borderWidth: 1,
                borderRadius: 4,
              }],
            }

            return (
              <div key={name} className="h-48">
                <Bar
                  data={chartData}
                  options={{
                    responsive: true,
                    maintainAspectRatio: false,
                    scales: {
                      x: { grid: { display: false }, ticks: { color: 'hsl(215, 20%, 60%)', font: { size: 10 } } },
                      y: {
                        grid: { color: 'hsl(217, 33%, 18%)' },
                        ticks: { color: 'hsl(215, 20%, 60%)', callback: (v) => formatNumber(v as number) },
                        title: { display: true, text: paramNameLabel(name), color: 'hsl(215, 20%, 60%)' },
                      },
                    },
                    plugins: {
                      legend: { display: false },
                      tooltip: {
                        callbacks: {
                          label: (ctx) => {
                            const t = trials[ctx.dataIndex]
                            const y = ctx.parsed.y ?? 0
                            const score = t.score ?? 0
                            return `${paramNameLabel(name)}: ${formatNumber(y)} | Score: ${formatNumber(score)}`
                          },
                        },
                      },
                    },
                  }}
                />
              </div>
            )
          })}
        </div>
        <div className="flex items-center justify-center gap-2 mt-3">
          <span className="text-xs text-muted-foreground">Low Score</span>
          <div
            className="h-3 w-24 rounded"
            style={{ background: 'linear-gradient(90deg, hsl(120, 65%, 48%), hsl(0, 65%, 48%))' }}
          />
          <span className="text-xs text-muted-foreground">High Score</span>
        </div>
        <div className="flex items-center justify-center gap-2 mt-1.5">
          <div className="h-3 w-5 rounded-sm shrink-0" style={{ background: 'hsl(0, 0%, 45%)' }} />
          <span className="text-xs text-muted-foreground">SLA Failed</span>
        </div>
      </CardContent>
    </Card>
  )
}
