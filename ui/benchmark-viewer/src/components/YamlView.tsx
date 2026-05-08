import type { ResultDetail } from '@/types'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'

interface YamlViewProps {
  data: ResultDetail
}

export function YamlView({ data }: YamlViewProps) {
  const jsonStr = JSON.stringify(data, null, 2)

  return (
    <Card className="animate-fade-in">
      <CardContent>
        <pre className="bg-secondary/30 rounded-md p-4 text-xs font-mono text-muted-foreground overflow-x-auto max-h-[70vh] overflow-y-auto whitespace-pre">
          {jsonStr}
        </pre>
      </CardContent>
    </Card>
  )
}
