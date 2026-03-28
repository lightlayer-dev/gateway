import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchMetrics } from '../lib/api'
import Chart from '../components/Chart'
import MetricCard from '../components/MetricCard'
import { BarChart3, Users, Clock, AlertTriangle } from 'lucide-react'
import {
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  Tooltip,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
} from 'recharts'

const PERIODS = [
  { label: '24h', value: '24h' },
  { label: '7d', value: '7d' },
  { label: '30d', value: '30d' },
] as const

const PIE_COLORS = ['#22c55e', '#3b82f6', '#f59e0b', '#ef4444']

function statusCategory(code: number): string {
  if (code < 200) return 'Other'
  if (code < 300) return '2xx'
  if (code < 400) return '3xx'
  if (code < 500) return '4xx'
  return '5xx'
}

export default function Analytics() {
  const [period, setPeriod] = useState('24h')
  const { data: metrics } = useQuery({
    queryKey: ['metrics', period],
    queryFn: () => fetchMetrics(period),
  })

  const chartData = (metrics?.requests_by_hour ?? []).map((h) => ({
    time: h.hour.replace(/^.*T(\d{2}):.*$/, '$1:00'),
    value: h.count,
  }))

  // Aggregate status codes into categories for pie chart.
  const statusData = (() => {
    if (!metrics?.status_distribution) return []
    const groups: Record<string, number> = {}
    for (const [code, count] of Object.entries(metrics.status_distribution)) {
      const cat = statusCategory(Number(code))
      groups[cat] = (groups[cat] ?? 0) + count
    }
    return Object.entries(groups).map(([name, value]) => ({ name, value }))
  })()

  // Response time histogram — bucket durations from top_paths avg_latency.
  const latencyBuckets = (() => {
    if (!metrics?.top_paths?.length) return []
    const buckets = [
      { label: '<50ms', min: 0, max: 50, count: 0 },
      { label: '50-100ms', min: 50, max: 100, count: 0 },
      { label: '100-250ms', min: 100, max: 250, count: 0 },
      { label: '250-500ms', min: 250, max: 500, count: 0 },
      { label: '500ms+', min: 500, max: Infinity, count: 0 },
    ]
    for (const p of metrics.top_paths) {
      const lat = p.avg_latency_ms ?? 0
      const bucket = buckets.find((b) => lat >= b.min && lat < b.max)
      if (bucket) bucket.count += p.count
    }
    return buckets.filter((b) => b.count > 0)
  })()

  const pieColor = (name: string) => {
    const map: Record<string, string> = { '2xx': PIE_COLORS[0], '3xx': PIE_COLORS[1], '4xx': PIE_COLORS[2], '5xx': PIE_COLORS[3] }
    return map[name] ?? '#94a3b8'
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-[var(--color-text)]">Analytics</h2>
        <div className="flex gap-1 bg-[var(--color-bg-secondary)] rounded-lg p-1">
          {PERIODS.map((p) => (
            <button
              key={p.value}
              onClick={() => setPeriod(p.value)}
              className={`px-3 py-1 text-sm rounded-md transition-colors ${
                period === p.value
                  ? 'bg-[var(--color-accent)] text-white'
                  : 'text-[var(--color-text-secondary)] hover:text-[var(--color-text)]'
              }`}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
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
        <MetricCard
          title="Avg Latency"
          value={metrics?.avg_latency_ms != null ? `${Math.round(metrics.avg_latency_ms)}ms` : '—'}
          icon={<Clock size={16} />}
        />
        <MetricCard
          title="Error Rate"
          value={metrics?.error_rate != null ? `${(metrics.error_rate * 100).toFixed(1)}%` : '—'}
          icon={<AlertTriangle size={16} />}
        />
      </div>

      {chartData.length > 0 && <Chart data={chartData} title="Requests over Time" />}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Status code distribution */}
        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">Status Code Distribution</h3>
          {statusData.length > 0 ? (
            <div className="flex items-center gap-6">
              <ResponsiveContainer width="50%" height={180}>
                <PieChart>
                  <Pie
                    data={statusData}
                    dataKey="value"
                    nameKey="name"
                    cx="50%"
                    cy="50%"
                    outerRadius={70}
                    strokeWidth={0}
                  >
                    {statusData.map((entry) => (
                      <Cell key={entry.name} fill={pieColor(entry.name)} />
                    ))}
                  </Pie>
                  <Tooltip
                    contentStyle={{
                      background: 'var(--color-bg)',
                      border: '1px solid var(--color-border)',
                      borderRadius: 6,
                      fontSize: 12,
                    }}
                  />
                </PieChart>
              </ResponsiveContainer>
              <div className="space-y-2">
                {statusData.map((d) => (
                  <div key={d.name} className="flex items-center gap-2 text-sm">
                    <span
                      className="w-3 h-3 rounded-full inline-block"
                      style={{ backgroundColor: pieColor(d.name) }}
                    />
                    <span className="text-[var(--color-text)]">{d.name}</span>
                    <span className="text-[var(--color-text-secondary)]">{d.value}</span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <p className="text-sm text-[var(--color-text-secondary)]">No data yet</p>
          )}
        </div>

        {/* Response time histogram */}
        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">Response Time Distribution</h3>
          {latencyBuckets.length > 0 ? (
            <ResponsiveContainer width="100%" height={180}>
              <BarChart data={latencyBuckets}>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
                <XAxis
                  dataKey="label"
                  tick={{ fontSize: 11, fill: 'var(--color-text-secondary)' }}
                  stroke="var(--color-border)"
                />
                <YAxis
                  tick={{ fontSize: 11, fill: 'var(--color-text-secondary)' }}
                  stroke="var(--color-border)"
                />
                <Tooltip
                  contentStyle={{
                    background: 'var(--color-bg)',
                    border: '1px solid var(--color-border)',
                    borderRadius: 6,
                    fontSize: 12,
                  }}
                />
                <Bar dataKey="count" fill="var(--color-accent)" radius={[4, 4, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          ) : (
            <p className="text-sm text-[var(--color-text-secondary)]">No data yet</p>
          )}
        </div>
      </div>

      {/* Top agents */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">Top Agents</h3>
        {metrics?.top_agents?.length ? (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-[var(--color-text-secondary)] border-b border-[var(--color-border)]">
                <th className="pb-2">Agent</th>
                <th className="pb-2 text-right">Requests</th>
                <th className="pb-2 text-right">Last Seen</th>
              </tr>
            </thead>
            <tbody>
              {metrics.top_agents.map((a) => (
                <tr key={a.agent} className="border-b border-[var(--color-border)] last:border-0">
                  <td className="py-2 text-[var(--color-text)]">{a.agent}</td>
                  <td className="py-2 text-right text-[var(--color-text)]">{a.count}</td>
                  <td className="py-2 text-right text-[var(--color-text-secondary)] text-xs">
                    {a.last_seen ? new Date(a.last_seen).toLocaleString() : '—'}
                  </td>
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
                <th className="pb-2">Method</th>
                <th className="pb-2 text-right">Requests</th>
                <th className="pb-2 text-right">Avg Latency</th>
              </tr>
            </thead>
            <tbody>
              {metrics.top_paths.map((p, i) => (
                <tr key={`${p.path}-${p.method}-${i}`} className="border-b border-[var(--color-border)] last:border-0">
                  <td className="py-2 text-[var(--color-text)] font-mono text-xs">{p.path}</td>
                  <td className="py-2 text-[var(--color-text-secondary)] text-xs font-mono">{p.method ?? '—'}</td>
                  <td className="py-2 text-right text-[var(--color-text)]">{p.count}</td>
                  <td className="py-2 text-right text-[var(--color-text-secondary)]">
                    {p.avg_latency_ms != null ? `${Math.round(p.avg_latency_ms)}ms` : '—'}
                  </td>
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
