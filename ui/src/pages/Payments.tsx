import { useQuery } from '@tanstack/react-query'
import { fetchConfig } from '../lib/api'

export default function Payments() {
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })

  const payments = (config?.plugins as Record<string, unknown>)?.payments as
    | Record<string, unknown>
    | undefined

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-[var(--color-text)]">Payments</h2>
      <p className="text-sm text-[var(--color-text-secondary)]">
        Configure x402 micropayment routes and pricing.
      </p>

      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">
          Payment Configuration
        </h3>
        {payments ? (
          <pre className="text-xs font-mono bg-[var(--color-bg-tertiary)] p-4 rounded overflow-x-auto text-[var(--color-text)]">
            {JSON.stringify(payments, null, 2)}
          </pre>
        ) : (
          <p className="text-sm text-[var(--color-text-secondary)]">Payments not configured</p>
        )}
      </div>
    </div>
  )
}
