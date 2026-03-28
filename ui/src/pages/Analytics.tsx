import { useQuery } from '@tanstack/react-query'
import { fetchMetrics } from '../lib/api'
import Chart from '../components/Chart'
import MetricCard from '../components/MetricCard'
import { BarChart3, Users } from 'lucide-react'

export default function Analytics() {
  const { data: metrics } = useQuery({
    queryKey: ['metrics', '24h'],
    queryFn: () => fetchMetrics('24h'),
  })

  const chartData = (metrics?.requests_by_hour ?? []).map((h) => ({
    time: h.hour,
    value: h.count,
  }))

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-[var(--color-text)]">Analytics</h2>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <MetricCard
          title="Total Requests"
          value={metrics?.total_requests ?? 0}
          icon={<BarChart3 size={16} />}
        />
        <MetricCard
          title="Unique Agents"
          value={metrics?.unique_agents ?? 0}
          icon={<Users size={16} />}
        />
      </div>

      {chartData.length > 0 && <Chart data={chartData} title="Requests over Time" />}

      {/* Top agents */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">Top Agents</h3>
        {metrics?.top_agents?.length ? (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-[var(--color-text-secondary)] border-b border-[var(--color-border)]">
                <th className="pb-2">Agent</th>
                <th className="pb-2 text-right">Requests</th>
              </tr>
            </thead>
            <tbody>
              {metrics.top_agents.map((a) => (
                <tr key={a.name} className="border-b border-[var(--color-border)] last:border-0">
                  <td className="py-2 text-[var(--color-text)]">{a.name}</td>
                  <td className="py-2 text-right text-[var(--color-text)]">{a.count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="text-sm text-[var(--color-text-secondary)]">No agent data yet</p>
        )}
      </div>

      {/* Top paths */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">Top Paths</h3>
        {metrics?.top_paths?.length ? (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-[var(--color-text-secondary)] border-b border-[var(--color-border)]">
                <th className="pb-2">Path</th>
                <th className="pb-2 text-right">Requests</th>
              </tr>
            </thead>
            <tbody>
              {metrics.top_paths.map((p) => (
                <tr key={p.path} className="border-b border-[var(--color-border)] last:border-0">
                  <td className="py-2 text-[var(--color-text)] font-mono text-xs">{p.path}</td>
                  <td className="py-2 text-right text-[var(--color-text)]">{p.count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="text-sm text-[var(--color-text-secondary)]">No path data yet</p>
        )}
      </div>
    </div>
  )
}
