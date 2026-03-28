import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { fetchConfig, updatePluginConfig, fetchRateLimitUsage } from '../lib/api'

interface PerAgentOverride {
  agent: string
  requests: number
  window: string
}

export default function RateLimits() {
  const queryClient = useQueryClient()
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })
  const { data: usage } = useQuery({
    queryKey: ['rate-limit-usage'],
    queryFn: fetchRateLimitUsage,
    refetchInterval: 10000,
  })

  const [enabled, setEnabled] = useState(true)
  const [defaultRequests, setDefaultRequests] = useState(100)
  const [defaultWindow, setDefaultWindow] = useState('1m')
  const [overrides, setOverrides] = useState<PerAgentOverride[]>([])
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  useEffect(() => {
    if (!config) return
    const rl = config.plugins?.rate_limits as Record<string, unknown> | undefined
    if (rl) {
      setEnabled(rl.enabled as boolean ?? true)
      const def = rl.default as Record<string, unknown> | undefined
      if (def) {
        setDefaultRequests((def.requests as number) || 100)
        setDefaultWindow((def.window as string) || '1m')
      }
      const pa = rl.per_agent as Record<string, Record<string, unknown>> | undefined
      if (pa) {
        setOverrides(
          Object.entries(pa).map(([agent, v]) => ({
            agent,
            requests: (v.requests as number) || 100,
            window: (v.window as string) || '1m',
          })),
        )
      }
    }
  }, [config])

  const saveMutation = useMutation({
    mutationFn: () => {
      if (!config) throw new Error('Config not loaded')
      const plugins = { ...config.plugins }
      const perAgent: Record<string, { requests: number; window: string }> = {}
      for (const o of overrides) {
        if (o.agent.trim()) {
          perAgent[o.agent.trim()] = { requests: o.requests, window: o.window }
        }
      }
      plugins.rate_limits = {
        enabled,
        default: { requests: defaultRequests, window: defaultWindow },
        per_agent: perAgent,
      }
      return updatePluginConfig(plugins)
    },
    onSuccess: () => {
      setMessage({ type: 'success', text: 'Rate limits saved and reloaded.' })
      queryClient.invalidateQueries({ queryKey: ['config'] })
      queryClient.invalidateQueries({ queryKey: ['rate-limit-usage'] })
      setTimeout(() => setMessage(null), 3000)
    },
    onError: (err: Error) => setMessage({ type: 'error', text: err.message }),
  })

  const addOverride = () => {
    setOverrides([...overrides, { agent: '', requests: 100, window: '1m' }])
  }

  const removeOverride = (index: number) => {
    setOverrides(overrides.filter((_, i) => i !== index))
  }

  const updateOverride = (index: number, field: keyof PerAgentOverride, value: unknown) => {
    const updated = [...overrides]
    updated[index] = { ...updated[index], [field]: value }
    setOverrides(updated)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-[var(--color-text)]">Rate Limits</h2>
          <p className="text-sm text-[var(--color-text-secondary)]">
            Configure rate limiting rules for agents and clients.
          </p>
        </div>
        <button
          onClick={() => saveMutation.mutate()}
          disabled={saveMutation.isPending}
          className="px-4 py-2 bg-[var(--color-accent)] text-white text-sm font-medium rounded-lg hover:opacity-90 disabled:opacity-50"
        >
          {saveMutation.isPending ? 'Saving...' : 'Save'}
        </button>
      </div>

      {message && (
        <div
          className={`text-sm px-4 py-2 rounded-lg ${
            message.type === 'success'
              ? 'bg-[var(--color-success)]/10 text-[var(--color-success)]'
              : 'bg-[var(--color-error)]/10 text-[var(--color-error)]'
          }`}
        >
          {message.text}
        </div>
      )}

      {/* Enable toggle */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
            className="w-4 h-4 accent-[var(--color-accent)]"
          />
          <span className="text-sm font-medium text-[var(--color-text)]">
            Enable rate limiting
          </span>
        </label>
      </div>

      {/* Default limits */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Default Limit</h3>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
              Requests
            </label>
            <input
              type="number"
              value={defaultRequests}
              onChange={(e) => setDefaultRequests(parseInt(e.target.value) || 0)}
              className="w-full px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
              Window
            </label>
            <select
              value={defaultWindow}
              onChange={(e) => setDefaultWindow(e.target.value)}
              className="w-full px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
            >
              <option value="30s">30 seconds</option>
              <option value="1m">1 minute</option>
              <option value="5m">5 minutes</option>
              <option value="15m">15 minutes</option>
              <option value="1h">1 hour</option>
            </select>
          </div>
        </div>
      </div>

      {/* Per-agent overrides */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">
            Per-Agent Overrides
          </h3>
          <button
            onClick={addOverride}
            className="text-xs px-3 py-1 bg-[var(--color-accent)] text-white rounded-md hover:opacity-90"
          >
            + Add Override
          </button>
        </div>

        {overrides.length === 0 && (
          <p className="text-sm text-[var(--color-text-secondary)]">
            No per-agent overrides. All agents use the default limit.
          </p>
        )}

        {overrides.map((o, i) => (
          <div
            key={i}
            className="flex items-end gap-3 border border-[var(--color-border)] rounded-lg p-3 bg-[var(--color-bg-tertiary)]"
          >
            <div className="flex-1">
              <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                Agent Name
              </label>
              <input
                type="text"
                value={o.agent}
                onChange={(e) => updateOverride(i, 'agent', e.target.value)}
                placeholder="ClaudeBot"
                className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
              />
            </div>
            <div className="w-28">
              <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                Requests
              </label>
              <input
                type="number"
                value={o.requests}
                onChange={(e) => updateOverride(i, 'requests', parseInt(e.target.value) || 0)}
                className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
              />
            </div>
            <div className="w-36">
              <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                Window
              </label>
              <select
                value={o.window}
                onChange={(e) => updateOverride(i, 'window', e.target.value)}
                className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
              >
                <option value="30s">30s</option>
                <option value="1m">1m</option>
                <option value="5m">5m</option>
                <option value="15m">15m</option>
                <option value="1h">1h</option>
              </select>
            </div>
            <button
              onClick={() => removeOverride(i)}
              className="text-[var(--color-error)] hover:opacity-70 text-sm pb-1"
            >
              Remove
            </button>
          </div>
        ))}
      </div>

      {/* Current usage bars */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">
          Current Usage
        </h3>

        {!usage?.enabled && (
          <p className="text-sm text-[var(--color-text-secondary)]">
            Rate limiting is disabled.
          </p>
        )}

        {usage?.enabled && (!usage.usage || usage.usage.length === 0) && (
          <p className="text-sm text-[var(--color-text-secondary)]">
            No agent traffic recorded in the current window.
          </p>
        )}

        {usage?.usage?.map((u) => (
          <div key={u.agent} className="space-y-1">
            <div className="flex items-center justify-between text-xs">
              <span className="font-medium text-[var(--color-text)]">{u.agent}</span>
              <span className="text-[var(--color-text-secondary)]">
                {u.used} / {u.limit} ({u.window})
              </span>
            </div>
            <div className="w-full h-2 bg-[var(--color-bg-tertiary)] rounded-full overflow-hidden">
              <div
                className="h-full rounded-full transition-all"
                style={{
                  width: `${Math.min(u.percent, 100)}%`,
                  backgroundColor:
                    u.percent >= 90
                      ? 'var(--color-error)'
                      : u.percent >= 70
                        ? 'var(--color-warning)'
                        : 'var(--color-success)',
                }}
              />
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
