import { useQuery } from '@tanstack/react-query'
import { fetchStatus, fetchMetrics } from '../lib/api'
import MetricCard from '../components/MetricCard'
import Chart from '../components/Chart'
import { Activity, Users, Clock, AlertTriangle, Circle, Link } from 'lucide-react'
import { NavLink } from 'react-router-dom'

export default function Overview() {
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: fetchStatus })
  const { data: metrics } = useQuery({
    queryKey: ['metrics'],
    queryFn: () => fetchMetrics('24h'),
  })

  const chartData = (metrics?.requests_by_hour ?? []).map((h) => ({
    time: h.hour,
    value: h.count,
  }))

  const isHealthy = status?.status === 'running'

  return (
    <div className="space-y-6">
      {/* Status banner */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Circle
            size={12}
            fill={isHealthy ? 'var(--color-success)' : 'var(--color-error)'}
            stroke="none"
          />
          <div>
            <div className="font-semibold text-[var(--color-text)]">
              Gateway {isHealthy ? 'Running' : 'Offline'}
            </div>
            <div className="text-sm text-[var(--color-text-secondary)]">
              {status?.origin_url ?? '—'} &middot; Uptime: {status?.uptime ?? '—'}
            </div>
          </div>
        </div>
        {status?.version && (
          <span className="text-xs bg-[var(--color-bg-tertiary)] text-[var(--color-text-secondary)] px-2 py-1 rounded">
            v{status.version}
          </span>
        )}
      </div>

      {/* Metric cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          title="Requests (24h)"
          value={metrics?.total_requests ?? 0}
          icon={<Activity size={16} />}
        />
        <MetricCard
          title="Active Agents"
          value={metrics?.unique_agents ?? 0}
          icon={<Users size={16} />}
        />
        <MetricCard
          title="Avg Response Time"
          value={metrics?.avg_latency_ms ? `${Math.round(metrics.avg_latency_ms)}ms` : '—'}
          icon={<Clock size={16} />}
        />
        <MetricCard
          title="Error Rate"
          value={
            metrics?.error_rate != null
              ? `${(metrics.error_rate * 100).toFixed(1)}%`
              : '—'
          }
          icon={<AlertTriangle size={16} />}
        />
      </div>

      {/* Request chart */}
      {chartData.length > 0 && <Chart data={chartData} title="Requests (last 24h)" />}

      {/* Origin health + active plugins */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">
            Active Plugins
          </h3>
          {status?.plugins?.length ? (
            <div className="space-y-2">
              {status.plugins.map((p) => (
                <div
                  key={p.name}
                  className="flex items-center gap-2 text-sm text-[var(--color-text)]"
                >
                  <Circle size={8} fill="var(--color-success)" stroke="none" />
                  {p.name}
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-[var(--color-text-secondary)]">No plugins active</p>
          )}
        </div>

        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">
            Quick Links
          </h3>
          <div className="space-y-2">
            {[
              { to: '/analytics', label: 'View Analytics' },
              { to: '/plugins', label: 'Manage Plugins' },
              { to: '/settings', label: 'Gateway Settings' },
              { to: '/logs', label: 'Live Logs' },
            ].map((link) => (
              <NavLink
                key={link.to}
                to={link.to}
                className="flex items-center gap-2 text-sm text-[var(--color-accent)] hover:underline"
              >
                <Link size={14} />
                {link.label}
              </NavLink>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
