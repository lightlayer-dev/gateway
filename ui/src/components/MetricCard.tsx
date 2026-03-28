import type { ReactNode } from 'react'

interface Props {
  title: string
  value: string | number
  subtitle?: string
  icon?: ReactNode
}

export default function MetricCard({ title, value, subtitle, icon }: Props) {
  return (
    <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
      <div className="flex items-center justify-between mb-3">
        <span className="text-sm text-[var(--color-text-secondary)]">{title}</span>
        {icon && <span className="text-[var(--color-text-secondary)]">{icon}</span>}
      </div>
      <div className="text-2xl font-semibold text-[var(--color-text)]">{value}</div>
      {subtitle && (
        <div className="text-xs text-[var(--color-text-secondary)] mt-1">{subtitle}</div>
      )}
    </div>
  )
}
