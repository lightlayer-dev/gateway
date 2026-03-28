import { useQuery } from '@tanstack/react-query'
import { fetchConfig } from '../lib/api'

export default function Discovery() {
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })

  const discovery = (config?.plugins as Record<string, unknown>)?.discovery as
    | Record<string, unknown>
    | undefined

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-[var(--color-text)]">Discovery</h2>
      <p className="text-sm text-[var(--color-text-secondary)]">
        Configure your API's discovery endpoints. These are auto-generated from your gateway config.
      </p>

      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Current Config</h3>
        {discovery ? (
          <pre className="text-xs font-mono bg-[var(--color-bg-tertiary)] p-4 rounded overflow-x-auto text-[var(--color-text)]">
            {JSON.stringify(discovery, null, 2)}
          </pre>
        ) : (
          <p className="text-sm text-[var(--color-text-secondary)]">Discovery plugin not configured</p>
        )}
      </div>

      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">
          Generated Endpoints
        </h3>
        <div className="space-y-2 text-sm">
          {['/.well-known/ai', '/.well-known/agent.json', '/agents.txt', '/llms.txt'].map(
            (ep) => (
              <div key={ep} className="font-mono text-xs text-[var(--color-accent)]">{ep}</div>
            ),
          )}
        </div>
      </div>
    </div>
  )
}
