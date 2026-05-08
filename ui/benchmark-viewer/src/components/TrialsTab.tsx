import type { TrialResult, TemplateDetail } from '@/types'
import { TrialTable } from './TrialTable'
import { ParallelCoordinates } from './ParallelCoordinates'

interface TrialsTabProps {
  trials: TrialResult[]
  optimize: string
  multiTemplate?: boolean
  allTemplates?: TemplateDetail[]
}

export function TrialsTab({ trials, optimize, multiTemplate, allTemplates }: TrialsTabProps) {
  if (multiTemplate && allTemplates) {
    // Flat-map all trials with template name included for the table
    const allTrials = allTemplates.flatMap(t =>
      t.trials.map(tr => ({ ...tr, templateName: t.name }))
    )
    return (
      <div className="space-y-6 animate-fade-in">
        <TrialTable
          trials={allTrials}
          optimize={optimize}
          showTemplateColumn
        />
      </div>
    )
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <ParallelCoordinates trials={trials} optimize={optimize} />
      <TrialTable trials={trials} optimize={optimize} />
    </div>
  )
}
