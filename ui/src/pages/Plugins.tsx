import { useQuery } from '@tanstack/react-query'
import { fetchStatus } from '../lib/api'
import { Circle } from 'lucide-react'

export default function Plugins() {
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: fetchStatus })

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-[var(--color-text)]">Plugins</h2>
      <p className="text-sm text-[var(--color-text-secondary)]">
        Manage gateway plugins. Toggle and configure each plugin below.
      </p>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        {status?.plugins?.map((p) => (
          <div
            key={p.name}
            className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 flex items-center justify-between"
          >
            <div className="flex items-center gap-3">
              <Circle
                size={10}
                fill={p.active ? 'var(--color-success)' : 'var(--color-text-secondary)'}
                stroke="none"
              />
              <span className="font-medium text-[var(--color-text)] capitalize">{p.name}</span>
            </div>
            <span
              className={`text-xs px-2 py-0.5 rounded ${
                p.active
                  ? 'bg-green-500/10 text-green-600 dark:text-green-400'
                  : 'bg-gray-500/10 text-gray-500'
              }`}
            >
              {p.active ? 'Active' : 'Inactive'}
            </span>
          </div>
        )) ?? (
          <p className="text-sm text-[var(--color-text-secondary)]">Loading plugins...</p>
        )}
      </div>
    </div>
  )
}
