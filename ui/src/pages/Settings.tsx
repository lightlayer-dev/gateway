import { useQuery } from '@tanstack/react-query'
import { fetchConfig, fetchStatus } from '../lib/api'

export default function Settings() {
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: fetchStatus })

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-[var(--color-text)]">Settings</h2>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-3">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Gateway</h3>
          <div className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-[var(--color-text-secondary)]">Listen Port</span>
              <span className="text-[var(--color-text)] font-mono">{status?.listen_port ?? '—'}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-[var(--color-text-secondary)]">Admin Port</span>
              <span className="text-[var(--color-text)] font-mono">{status?.admin_port ?? '—'}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-[var(--color-text-secondary)]">Origin URL</span>
              <span className="text-[var(--color-text)] font-mono text-xs">{status?.origin_url ?? '—'}</span>
            </div>
          </div>
        </div>

        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-3">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Runtime</h3>
          <div className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-[var(--color-text-secondary)]">Version</span>
              <span className="text-[var(--color-text)]">{status?.version ?? '—'}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-[var(--color-text-secondary)]">Uptime</span>
              <span className="text-[var(--color-text)]">{status?.uptime ?? '—'}</span>
            </div>
          </div>
        </div>
      </div>

      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">
          Full Configuration (YAML)
        </h3>
        {config ? (
          <pre className="text-xs font-mono bg-[var(--color-bg-tertiary)] p-4 rounded overflow-x-auto text-[var(--color-text)]">
            {JSON.stringify(config, null, 2)}
          </pre>
        ) : (
          <p className="text-sm text-[var(--color-text-secondary)]">Loading...</p>
        )}
      </div>
    </div>
  )
}
