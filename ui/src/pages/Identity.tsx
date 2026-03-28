import { useQuery } from '@tanstack/react-query'
import { fetchConfig } from '../lib/api'

export default function Identity() {
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })

  const identity = (config?.plugins as Record<string, unknown>)?.identity as
    | Record<string, unknown>
    | undefined

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-[var(--color-text)]">Identity</h2>
      <p className="text-sm text-[var(--color-text-secondary)]">
        Manage agent identity verification — JWT, SPIFFE, and WIMSE.
      </p>

      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">
          Verification Mode
        </h3>
        {identity ? (
          <pre className="text-xs font-mono bg-[var(--color-bg-tertiary)] p-4 rounded overflow-x-auto text-[var(--color-text)]">
            {JSON.stringify(identity, null, 2)}
          </pre>
        ) : (
          <p className="text-sm text-[var(--color-text-secondary)]">Identity plugin not configured</p>
        )}
      </div>
    </div>
  )
}
