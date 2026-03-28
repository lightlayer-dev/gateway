import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { fetchConfig, updatePluginConfig, fetchAgentActivity } from '../lib/api'

const MODES = [
  {
    value: 'log',
    label: 'Log',
    description: 'Observe and log agent identity claims without taking action.',
  },
  {
    value: 'warn',
    label: 'Warn',
    description: 'Log identity claims and add warning headers to responses for unverified agents.',
  },
  {
    value: 'enforce',
    label: 'Enforce',
    description: 'Reject requests from agents that cannot be verified. Strictest mode.',
  },
] as const

export default function Identity() {
  const queryClient = useQueryClient()
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })
  const { data: activity } = useQuery({
    queryKey: ['agent-activity'],
    queryFn: fetchAgentActivity,
    refetchInterval: 15000,
  })

  const [enabled, setEnabled] = useState(true)
  const [mode, setMode] = useState('log')
  const [trustedIssuers, setTrustedIssuers] = useState<string[]>([])
  const [newIssuer, setNewIssuer] = useState('')
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  useEffect(() => {
    if (!config) return
    const id = config.plugins?.identity as Record<string, unknown> | undefined
    if (id) {
      setEnabled(id.enabled as boolean ?? true)
      setMode((id.mode as string) || 'log')
      setTrustedIssuers((id.trusted_issuers as string[]) || [])
    }
  }, [config])

  const saveMutation = useMutation({
    mutationFn: () => {
      if (!config) throw new Error('Config not loaded')
      const plugins = { ...config.plugins }
      plugins.identity = {
        ...plugins.identity,
        enabled,
        mode,
        trusted_issuers: trustedIssuers,
      }
      return updatePluginConfig(plugins)
    },
    onSuccess: () => {
      setMessage({ type: 'success', text: 'Identity config saved and reloaded.' })
      queryClient.invalidateQueries({ queryKey: ['config'] })
      setTimeout(() => setMessage(null), 3000)
    },
    onError: (err: Error) => setMessage({ type: 'error', text: err.message }),
  })

  const addIssuer = () => {
    const url = newIssuer.trim()
    if (url && !trustedIssuers.includes(url)) {
      setTrustedIssuers([...trustedIssuers, url])
      setNewIssuer('')
    }
  }

  const removeIssuer = (index: number) => {
    setTrustedIssuers(trustedIssuers.filter((_, i) => i !== index))
  }

  const formatTime = (ts: string) => {
    if (!ts) return '—'
    const d = new Date(ts)
    return d.toLocaleString()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-[var(--color-text)]">Identity</h2>
          <p className="text-sm text-[var(--color-text-secondary)]">
            Manage agent identity verification — JWT, SPIFFE, and WIMSE.
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
            Enable identity verification
          </span>
        </label>
      </div>

      {/* Mode selector */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">
          Verification Mode
        </h3>
        <div className="space-y-3">
          {MODES.map((m) => (
            <label
              key={m.value}
              className={`flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors ${
                mode === m.value
                  ? 'border-[var(--color-accent)] bg-[var(--color-accent)]/5'
                  : 'border-[var(--color-border)] hover:border-[var(--color-text-secondary)]'
              }`}
            >
              <input
                type="radio"
                name="identity-mode"
                value={m.value}
                checked={mode === m.value}
                onChange={(e) => setMode(e.target.value)}
                className="mt-0.5 accent-[var(--color-accent)]"
              />
              <div>
                <div className="text-sm font-medium text-[var(--color-text)]">{m.label}</div>
                <div className="text-xs text-[var(--color-text-secondary)]">{m.description}</div>
              </div>
            </label>
          ))}
        </div>
      </div>

      {/* Trusted issuers */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Trusted Issuers</h3>
        <div className="flex gap-2">
          <input
            type="text"
            value={newIssuer}
            onChange={(e) => setNewIssuer(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && addIssuer()}
            placeholder="https://auth.example.com"
            className="flex-1 px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
          />
          <button
            onClick={addIssuer}
            className="px-3 py-2 text-sm bg-[var(--color-accent)] text-white rounded-lg hover:opacity-90"
          >
            Add
          </button>
        </div>

        {trustedIssuers.length === 0 && (
          <p className="text-sm text-[var(--color-text-secondary)]">
            No trusted issuers configured. All JWT issuers will be accepted.
          </p>
        )}

        <div className="space-y-2">
          {trustedIssuers.map((issuer, i) => (
            <div
              key={i}
              className="flex items-center justify-between px-3 py-2 bg-[var(--color-bg-tertiary)] rounded-lg"
            >
              <span className="text-sm font-mono text-[var(--color-text)]">{issuer}</span>
              <button
                onClick={() => removeIssuer(i)}
                className="text-xs text-[var(--color-error)] hover:opacity-70"
              >
                Remove
              </button>
            </div>
          ))}
        </div>
      </div>

      {/* Agent activity table */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">
          Agent Activity
        </h3>

        {(!activity?.agents || activity.agents.length === 0) && (
          <p className="text-sm text-[var(--color-text-secondary)]">
            No agents detected yet.
          </p>
        )}

        {activity?.agents && activity.agents.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-[var(--color-border)]">
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    Agent
                  </th>
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    Requests
                  </th>
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    First Seen
                  </th>
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    Last Seen
                  </th>
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    Verified
                  </th>
                </tr>
              </thead>
              <tbody>
                {activity.agents.map((agent) => (
                  <tr
                    key={agent.name}
                    className="border-b border-[var(--color-border)] last:border-0"
                  >
                    <td className="py-2 px-3 font-medium text-[var(--color-text)]">
                      {agent.name}
                    </td>
                    <td className="py-2 px-3 text-[var(--color-text-secondary)]">
                      {agent.total_requests.toLocaleString()}
                    </td>
                    <td className="py-2 px-3 text-[var(--color-text-secondary)] text-xs">
                      {formatTime(agent.first_seen)}
                    </td>
                    <td className="py-2 px-3 text-[var(--color-text-secondary)] text-xs">
                      {formatTime(agent.last_seen)}
                    </td>
                    <td className="py-2 px-3">
                      <span
                        className={`inline-block w-2 h-2 rounded-full ${
                          agent.verified ? 'bg-[var(--color-success)]' : 'bg-[var(--color-text-secondary)]'
                        }`}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
