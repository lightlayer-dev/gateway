import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { fetchConfig, updatePluginConfig } from '../lib/api'
import { Circle, ChevronDown, ChevronRight, Save, Loader2 } from 'lucide-react'

const PLUGIN_DESCRIPTIONS: Record<string, string> = {
  discovery: 'Serves /.well-known/ai, /agents.txt, /llms.txt discovery endpoints',
  identity: 'Agent identity verification via JWT, SPIFFE, WIMSE',
  rate_limits: 'Per-agent rate limiting with sliding window',
  payments: 'x402 payment negotiation for paid API routes',
  analytics: 'Traffic telemetry with batch flushing and storage',
  security: 'CORS, HSTS, CSP, and security headers',
  oauth2: 'OAuth2 PKCE flow with discovery endpoint',
  mcp: 'Auto-generated MCP tools from discovery config',
  a2a: 'Full Google A2A protocol server (JSON-RPC 2.0)',
  ag_ui: 'AG-UI SSE streaming for CopilotKit/ADK',
  api_keys: 'Scoped API key authentication and management',
  agents_txt: 'Per-agent access control rules',
}

export default function Plugins() {
  const queryClient = useQueryClient()
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })
  const [expanded, setExpanded] = useState<string | null>(null)
  const [localPlugins, setLocalPlugins] = useState<Record<string, unknown> | null>(null)
  const [dirty, setDirty] = useState(false)

  const plugins = localPlugins ?? config?.plugins ?? {}

  const saveMutation = useMutation({
    mutationFn: () => updatePluginConfig(plugins),
    onSuccess: () => {
      setDirty(false)
      setLocalPlugins(null)
      queryClient.invalidateQueries({ queryKey: ['config'] })
      queryClient.invalidateQueries({ queryKey: ['status'] })
    },
  })

  const togglePlugin = (name: string) => {
    const current = plugins[name] as Record<string, unknown> | undefined
    const updated = { ...plugins, [name]: { ...current, enabled: !(current?.enabled ?? false) } }
    setLocalPlugins(updated)
    setDirty(true)
  }

  const pluginEntries = Object.entries(plugins).filter(
    ([, v]) => typeof v === 'object' && v !== null
  ) as [string, Record<string, unknown>][]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-[var(--color-text)]">Plugins</h2>
          <p className="text-sm text-[var(--color-text-secondary)]">
            Toggle and configure gateway plugins.
          </p>
        </div>
        {dirty && (
          <button
            onClick={() => saveMutation.mutate()}
            disabled={saveMutation.isPending}
            className="flex items-center gap-2 px-4 py-2 bg-[var(--color-accent)] text-white text-sm rounded-lg hover:opacity-90 disabled:opacity-50 transition-opacity"
          >
            {saveMutation.isPending ? (
              <Loader2 size={14} className="animate-spin" />
            ) : (
              <Save size={14} />
            )}
            Save Changes
          </button>
        )}
      </div>

      {saveMutation.isError && (
        <div className="bg-red-500/10 border border-red-500/20 text-red-600 dark:text-red-400 text-sm rounded-lg p-3">
          Save failed: {saveMutation.error?.message ?? 'Unknown error'}
        </div>
      )}

      {saveMutation.isSuccess && (
        <div className="bg-green-500/10 border border-green-500/20 text-green-600 dark:text-green-400 text-sm rounded-lg p-3">
          Plugin configuration saved and reloaded.
        </div>
      )}

      <div className="space-y-3">
        {pluginEntries.map(([name, pluginCfg]) => {
          const enabled = pluginCfg.enabled === true
          const isExpanded = expanded === name
          const status = enabled ? 'active' : 'disabled'

          return (
            <div
              key={name}
              className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] overflow-hidden"
            >
              <div className="p-4 flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <button
                    onClick={() => setExpanded(isExpanded ? null : name)}
                    className="text-[var(--color-text-secondary)] hover:text-[var(--color-text)]"
                  >
                    {isExpanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                  </button>
                  <Circle
                    size={10}
                    fill={enabled ? 'var(--color-success)' : 'var(--color-text-secondary)'}
                    stroke="none"
                  />
                  <div>
                    <span className="font-medium text-[var(--color-text)] capitalize">{name.replace(/_/g, ' ')}</span>
                    <p className="text-xs text-[var(--color-text-secondary)]">
                      {PLUGIN_DESCRIPTIONS[name] ?? ''}
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <span
                    className={`text-xs px-2 py-0.5 rounded ${
                      status === 'active'
                        ? 'bg-green-500/10 text-green-600 dark:text-green-400'
                        : 'bg-gray-500/10 text-gray-500'
                    }`}
                  >
                    {status}
                  </span>
                  <button
                    onClick={() => togglePlugin(name)}
                    className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                      enabled ? 'bg-[var(--color-accent)]' : 'bg-gray-400'
                    }`}
                  >
                    <span
                      className={`inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform ${
                        enabled ? 'translate-x-4.5' : 'translate-x-0.5'
                      }`}
                    />
                  </button>
                </div>
              </div>

              {isExpanded && (
                <div className="border-t border-[var(--color-border)] p-4 bg-[var(--color-bg-secondary)]">
                  <h4 className="text-xs font-medium text-[var(--color-text-secondary)] mb-2 uppercase tracking-wide">
                    Configuration
                  </h4>
                  <pre className="text-xs font-mono bg-[var(--color-bg-tertiary)] p-3 rounded overflow-x-auto text-[var(--color-text)]">
                    {JSON.stringify(pluginCfg, null, 2)}
                  </pre>
                </div>
              )}
            </div>
          )
        })}

        {pluginEntries.length === 0 && (
          <p className="text-sm text-[var(--color-text-secondary)]">Loading plugins...</p>
        )}
      </div>
    </div>
  )
}
