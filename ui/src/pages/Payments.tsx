import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { fetchConfig, updatePluginConfig, fetchPaymentHistory } from '../lib/api'

interface PaymentRoute {
  path: string
  price: string
  currency: string
  description: string
}

export default function Payments() {
  const queryClient = useQueryClient()
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })
  const { data: history } = useQuery({
    queryKey: ['payment-history'],
    queryFn: () => fetchPaymentHistory(50),
    refetchInterval: 15000,
  })

  const [enabled, setEnabled] = useState(false)
  const [facilitator, setFacilitator] = useState('')
  const [routes, setRoutes] = useState<PaymentRoute[]>([])
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  useEffect(() => {
    if (!config) return
    const p = config.plugins?.payments as Record<string, unknown> | undefined
    if (p) {
      setEnabled(p.enabled as boolean ?? false)
      setFacilitator((p.facilitator as string) || '')
      const r = p.routes as PaymentRoute[] | undefined
      if (r) {
        setRoutes(r.map((rt) => ({
          path: rt.path || '',
          price: rt.price || '',
          currency: rt.currency || 'USDC',
          description: rt.description || '',
        })))
      }
    }
  }, [config])

  const saveMutation = useMutation({
    mutationFn: () => {
      if (!config) throw new Error('Config not loaded')
      const plugins = { ...config.plugins }
      plugins.payments = {
        ...plugins.payments,
        enabled,
        facilitator,
        routes: routes.filter((r) => r.path.trim() && r.price.trim()),
      }
      return updatePluginConfig(plugins)
    },
    onSuccess: () => {
      setMessage({ type: 'success', text: 'Payment config saved and reloaded.' })
      queryClient.invalidateQueries({ queryKey: ['config'] })
      setTimeout(() => setMessage(null), 3000)
    },
    onError: (err: Error) => setMessage({ type: 'error', text: err.message }),
  })

  const addRoute = () => {
    setRoutes([...routes, { path: '', price: '', currency: 'USDC', description: '' }])
  }

  const removeRoute = (index: number) => {
    setRoutes(routes.filter((_, i) => i !== index))
  }

  const updateRoute = (index: number, field: keyof PaymentRoute, value: string) => {
    const updated = [...routes]
    updated[index] = { ...updated[index], [field]: value }
    setRoutes(updated)
  }

  const formatTime = (ts: string) => {
    if (!ts) return '—'
    const d = new Date(ts)
    return d.toLocaleString()
  }

  const parsePaymentInfo = (info: string): Record<string, unknown> => {
    try {
      return JSON.parse(info)
    } catch {
      return { raw: info }
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-[var(--color-text)]">Payments</h2>
          <p className="text-sm text-[var(--color-text-secondary)]">
            Configure x402 micropayment routes and pricing.
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
            Enable x402 payments
          </span>
        </label>
      </div>

      {/* Facilitator URL */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Facilitator</h3>
        <div>
          <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
            Facilitator URL
          </label>
          <input
            type="text"
            value={facilitator}
            onChange={(e) => setFacilitator(e.target.value)}
            placeholder="https://x402.org/facilitator"
            className="w-full px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
          />
          <p className="text-xs text-[var(--color-text-secondary)] mt-1">
            The payment facilitator that verifies and settles payments.
          </p>
        </div>
      </div>

      {/* Paid routes */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Paid Routes</h3>
          <button
            onClick={addRoute}
            className="text-xs px-3 py-1 bg-[var(--color-accent)] text-white rounded-md hover:opacity-90"
          >
            + Add Route
          </button>
        </div>

        {routes.length === 0 && (
          <p className="text-sm text-[var(--color-text-secondary)]">
            No paid routes configured. Add a route to require payment for specific paths.
          </p>
        )}

        {routes.map((route, i) => (
          <div
            key={i}
            className="border border-[var(--color-border)] rounded-lg p-4 space-y-3 bg-[var(--color-bg-tertiary)]"
          >
            <div className="flex items-start justify-between">
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3 flex-1 mr-3">
                <div>
                  <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                    Path Pattern
                  </label>
                  <input
                    type="text"
                    value={route.path}
                    onChange={(e) => updateRoute(i, 'path', e.target.value)}
                    placeholder="/api/premium/*"
                    className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                    Price
                  </label>
                  <input
                    type="text"
                    value={route.price}
                    onChange={(e) => updateRoute(i, 'price', e.target.value)}
                    placeholder="0.01"
                    className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                    Currency
                  </label>
                  <select
                    value={route.currency}
                    onChange={(e) => updateRoute(i, 'currency', e.target.value)}
                    className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
                  >
                    <option value="USDC">USDC</option>
                    <option value="ETH">ETH</option>
                    <option value="BTC">BTC</option>
                    <option value="USD">USD</option>
                  </select>
                </div>
                <div>
                  <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                    Description
                  </label>
                  <input
                    type="text"
                    value={route.description}
                    onChange={(e) => updateRoute(i, 'description', e.target.value)}
                    placeholder="Premium API access"
                    className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
                  />
                </div>
              </div>
              <button
                onClick={() => removeRoute(i)}
                className="text-[var(--color-error)] hover:opacity-70 text-sm mt-5"
              >
                Remove
              </button>
            </div>
          </div>
        ))}
      </div>

      {/* Payment history */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Payment History</h3>

        {(!history?.payments || history.payments.length === 0) && (
          <p className="text-sm text-[var(--color-text-secondary)]">
            No payment events recorded yet.
          </p>
        )}

        {history?.payments && history.payments.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-[var(--color-border)]">
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    Time
                  </th>
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    Agent
                  </th>
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    Path
                  </th>
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    Status
                  </th>
                  <th className="text-left py-2 px-3 text-xs font-medium text-[var(--color-text-secondary)]">
                    Payment Info
                  </th>
                </tr>
              </thead>
              <tbody>
                {history.payments.map((p) => {
                  const info = parsePaymentInfo(p.payment_info)
                  return (
                    <tr
                      key={p.id}
                      className="border-b border-[var(--color-border)] last:border-0"
                    >
                      <td className="py-2 px-3 text-xs text-[var(--color-text-secondary)]">
                        {formatTime(p.timestamp)}
                      </td>
                      <td className="py-2 px-3 text-[var(--color-text)]">
                        {p.agent || '—'}
                      </td>
                      <td className="py-2 px-3 font-mono text-xs text-[var(--color-text-secondary)]">
                        {p.path}
                      </td>
                      <td className="py-2 px-3">
                        <span
                          className={`text-xs px-2 py-0.5 rounded ${
                            p.status_code === 200
                              ? 'bg-[var(--color-success)]/10 text-[var(--color-success)]'
                              : p.status_code === 402
                                ? 'bg-[var(--color-warning)]/10 text-[var(--color-warning)]'
                                : 'bg-[var(--color-error)]/10 text-[var(--color-error)]'
                          }`}
                        >
                          {p.status_code}
                        </span>
                      </td>
                      <td className="py-2 px-3 text-xs font-mono text-[var(--color-text-secondary)] max-w-xs truncate">
                        {info.amount
                          ? `${info.amount as string} ${(info.currency as string) || ''}`
                          : p.payment_info.slice(0, 60)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
