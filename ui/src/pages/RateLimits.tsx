import { useQuery } from '@tanstack/react-query'
import { fetchConfig } from '../lib/api'

export default function RateLimits() {
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })

  const rl = (config?.plugins as Record<string, unknown>)?.rate_limits as
    | Record<string, unknown>
    | undefined

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-[var(--color-text)]">Rate Limits</h2>
      <p className="text-sm text-[var(--color-text-secondary)]">
        Configure rate limiting rules for agents and clients.
      </p>

      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">
          Current Configuration
        </h3>
        {rl ? (
          <pre className="text-xs font-mono bg-[var(--color-bg-tertiary)] p-4 rounded overflow-x-auto text-[var(--color-text)]">
            {JSON.stringify(rl, null, 2)}
          </pre>
        ) : (
          <p className="text-sm text-[var(--color-text-secondary)]">Rate limiting not configured</p>
        )}
      </div>
    </div>
  )
}
