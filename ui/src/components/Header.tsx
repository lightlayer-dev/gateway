import { useQuery } from '@tanstack/react-query'
import { fetchHealth } from '../lib/api'
import { Circle } from 'lucide-react'

export default function Header() {
  const { data: health } = useQuery({
    queryKey: ['health'],
    queryFn: fetchHealth,
  })

  const isHealthy = health?.status === 'ok'

  return (
    <header className="h-14 px-6 flex items-center justify-between border-b border-[var(--color-border)] bg-[var(--color-bg)]">
      <div className="flex items-center gap-3">
        <h1 className="text-base font-semibold text-[var(--color-text)]">
          Gateway Dashboard
        </h1>
      </div>
      <div className="flex items-center gap-2 text-sm">
        <Circle
          size={10}
          fill={isHealthy ? 'var(--color-success)' : 'var(--color-error)'}
          stroke="none"
        />
        <span className="text-[var(--color-text-secondary)]">
          {isHealthy ? 'Healthy' : 'Unreachable'}
        </span>
        {health?.version && (
          <span className="text-[var(--color-text-secondary)] ml-2 text-xs bg-[var(--color-bg-tertiary)] px-2 py-0.5 rounded">
            v{health.version}
          </span>
        )}
      </div>
    </header>
  )
}
