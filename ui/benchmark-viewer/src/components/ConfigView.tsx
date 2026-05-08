import type { ResultDetail } from '@/types'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { formatNumber } from '@/lib/utils'

interface ConfigViewProps {
  config: ResultDetail['config']
}

export function ConfigView({ config }: ConfigViewProps) {
  return (
    <div className="space-y-6 animate-fade-in">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Experiment Identity</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <ConfigItem label="Experiment" value={config.name} />
            <ConfigItem label="Backend" value={config.backend} />
            <ConfigItem label="Algorithm" value={config.algorithm} />
            <ConfigItem label="Optimize Target" value={config.optimize} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">SLA Constraints</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-3 gap-3">
            <ConfigItem label="TTFT P99 Max" value={config.sla.ttftP99MaxMs != null ? `${config.sla.ttftP99MaxMs}ms` : 'Unlimited'} />
            <ConfigItem label="TPOT P99 Max" value={config.sla.tpotP99MaxMs != null ? `${config.sla.tpotP99MaxMs}ms` : 'Unlimited'} />
            <ConfigItem label="Error Rate Max" value={config.sla.errorRateMax != null ? `${formatNumber(config.sla.errorRateMax * 100)}%` : 'Unlimited'} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Scenario</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <ConfigItem label="Scenario" value={config.scenarioName} />
            <ConfigItem label="Workloads" value={config.scenarioWorkloads.join(', ')} />
            <ConfigItem label="Concurrency" value={config.scenarioConcurrency.join(', ')} />
            <ConfigItem label="Templates" value={config.templates.join(', ')} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Strategy</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <ConfigItem label="Max Trials" value={String(config.maxTrialsPerTemplate)} />
            <ConfigItem label="Early Stop" value={config.earlyStopPatience > 0 ? String(config.earlyStopPatience) : 'Disabled'} />
            <ConfigItem label="Timeout" value={config.timeout || 'None'} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Search Space</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {Object.entries(config.searchSpace).map(([role, params]) => (
            <div key={role}>
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Role: {role}</div>
              <div className="space-y-3">
                {Object.entries(params).map(([name, detail]) => (
                  <div key={name} className="bg-secondary/30 rounded-md p-3">
                    <div className="flex items-center gap-2 mb-1.5">
                      <span className="text-sm font-medium font-mono">{name}</span>
                      <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded uppercase ${
                        detail.type === 'range'
                          ? 'bg-info/10 text-info border border-info/20'
                          : 'bg-warning/10 text-warning border border-warning/20'
                      }`}>
                        {detail.type}
                      </span>
                    </div>
                    {detail.type === 'range' && detail.min != null && detail.max != null ? (
                      <div className="text-sm font-mono text-muted-foreground">
                        {formatNumber(detail.min)}
                        <span className="mx-1.5 text-xs">&ndash;</span>
                        {formatNumber(detail.max)}
                        {detail.step != null && (
                          <span className="ml-2 text-xs opacity-60">(step: {formatNumber(detail.step)})</span>
                        )}
                      </div>
                    ) : detail.values ? (
                      <div className="flex flex-wrap gap-1">
                        {detail.values.map((v, i) => (
                          <span key={i} className="bg-secondary/60 rounded px-1.5 py-0.5 text-xs font-mono">
                            {typeof v === 'number' ? formatNumber(v) : String(v)}
                          </span>
                        ))}
                      </div>
                    ) : (
                      <div className="text-xs text-muted-foreground italic">No range/value info</div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  )
}

function ConfigItem({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground mb-0.5">{label}</div>
      <div className="text-sm font-medium font-mono">{value}</div>
    </div>
  )
}
