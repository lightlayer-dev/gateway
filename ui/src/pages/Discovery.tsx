import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { fetchConfig, updatePluginConfig, fetchDiscoveryPreview } from '../lib/api'
import type { Capability } from '../lib/api'

export default function Discovery() {
  const queryClient = useQueryClient()
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })
  const { data: preview, refetch: refetchPreview } = useQuery({
    queryKey: ['discovery-preview'],
    queryFn: fetchDiscoveryPreview,
  })

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [version, setVersion] = useState('')
  const [capabilities, setCapabilities] = useState<Capability[]>([])
  const [previewTab, setPreviewTab] = useState<'ai' | 'agent' | 'agents_txt' | 'llms_txt'>('ai')
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

  // Sync form state from config
  useEffect(() => {
    if (!config) return
    const d = config.plugins?.discovery as Record<string, unknown> | undefined
    if (d) {
      setName((d.name as string) || '')
      setDescription((d.description as string) || '')
      setVersion((d.version as string) || '')
      setCapabilities((d.capabilities as Capability[]) || [])
    }
  }, [config])

  const saveMutation = useMutation({
    mutationFn: () => {
      if (!config) throw new Error('Config not loaded')
      const plugins = { ...config.plugins }
      plugins.discovery = {
        ...plugins.discovery,
        enabled: true,
        name,
        description,
        version,
        capabilities,
      }
      return updatePluginConfig(plugins)
    },
    onSuccess: () => {
      setMessage({ type: 'success', text: 'Discovery config saved and reloaded.' })
      queryClient.invalidateQueries({ queryKey: ['config'] })
      refetchPreview()
      setTimeout(() => setMessage(null), 3000)
    },
    onError: (err: Error) => {
      setMessage({ type: 'error', text: err.message })
    },
  })

  const addCapability = () => {
    setCapabilities([...capabilities, { name: '', description: '', methods: ['GET'], paths: ['/'] }])
  }

  const removeCapability = (index: number) => {
    setCapabilities(capabilities.filter((_, i) => i !== index))
  }

  const updateCapability = (index: number, field: keyof Capability, value: unknown) => {
    const updated = [...capabilities]
    updated[index] = { ...updated[index], [field]: value }
    setCapabilities(updated)
  }

  const previewContent: Record<string, string> = {
    ai: preview ? JSON.stringify(preview.well_known_ai, null, 2) : '{}',
    agent: preview ? JSON.stringify(preview.agent_card, null, 2) : '{}',
    agents_txt: preview?.agents_txt || '',
    llms_txt: preview?.llms_txt || '',
  }

  const previewLabels: Record<string, string> = {
    ai: '/.well-known/ai',
    agent: '/.well-known/agent.json',
    agents_txt: '/agents.txt',
    llms_txt: '/llms.txt',
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-[var(--color-text)]">Discovery</h2>
          <p className="text-sm text-[var(--color-text-secondary)]">
            Configure your API's discovery endpoints. Changes auto-generate all discovery formats.
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

      {/* API Info */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">API Information</h3>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div>
            <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
              Name
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="My API"
              className="w-full px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
              Version
            </label>
            <input
              type="text"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              placeholder="1.0.0"
              className="w-full px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
            />
          </div>
          <div className="sm:col-span-1">
            <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
              Description
            </label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="A REST API for..."
              className="w-full px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-lg text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
            />
          </div>
        </div>
      </div>

      {/* Capabilities */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Capabilities</h3>
          <button
            onClick={addCapability}
            className="text-xs px-3 py-1 bg-[var(--color-accent)] text-white rounded-md hover:opacity-90"
          >
            + Add Capability
          </button>
        </div>

        {capabilities.length === 0 && (
          <p className="text-sm text-[var(--color-text-secondary)]">
            No capabilities defined. Add one to describe your API's endpoints.
          </p>
        )}

        {capabilities.map((cap, i) => (
          <div
            key={i}
            className="border border-[var(--color-border)] rounded-lg p-4 space-y-3 bg-[var(--color-bg-tertiary)]"
          >
            <div className="flex items-start justify-between">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 flex-1 mr-3">
                <div>
                  <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                    Name
                  </label>
                  <input
                    type="text"
                    value={cap.name}
                    onChange={(e) => updateCapability(i, 'name', e.target.value)}
                    placeholder="widgets"
                    className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                    Description
                  </label>
                  <input
                    type="text"
                    value={cap.description}
                    onChange={(e) => updateCapability(i, 'description', e.target.value)}
                    placeholder="CRUD operations for widgets"
                    className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                    Methods (comma-separated)
                  </label>
                  <input
                    type="text"
                    value={cap.methods.join(', ')}
                    onChange={(e) =>
                      updateCapability(
                        i,
                        'methods',
                        e.target.value.split(',').map((s) => s.trim()).filter(Boolean),
                      )
                    }
                    placeholder="GET, POST, PUT, DELETE"
                    className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-[var(--color-text-secondary)] mb-1">
                    Paths (comma-separated)
                  </label>
                  <input
                    type="text"
                    value={cap.paths.join(', ')}
                    onChange={(e) =>
                      updateCapability(
                        i,
                        'paths',
                        e.target.value.split(',').map((s) => s.trim()).filter(Boolean),
                      )
                    }
                    placeholder="/api/widgets, /api/widgets/*"
                    className="w-full px-3 py-1.5 text-sm bg-[var(--color-bg)] border border-[var(--color-border)] rounded text-[var(--color-text)] focus:outline-none focus:border-[var(--color-accent)]"
                  />
                </div>
              </div>
              <button
                onClick={() => removeCapability(i)}
                className="text-[var(--color-error)] hover:opacity-70 text-sm mt-5"
              >
                Remove
              </button>
            </div>
          </div>
        ))}
      </div>

      {/* Live Preview */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">
          Live Preview — Generated Endpoints
        </h3>
        <div className="flex gap-1 border-b border-[var(--color-border)]">
          {(Object.keys(previewLabels) as Array<keyof typeof previewLabels>).map((key) => (
            <button
              key={key}
              onClick={() => setPreviewTab(key as typeof previewTab)}
              className={`px-3 py-2 text-xs font-mono transition-colors ${
                previewTab === key
                  ? 'text-[var(--color-accent)] border-b-2 border-[var(--color-accent)]'
                  : 'text-[var(--color-text-secondary)] hover:text-[var(--color-text)]'
              }`}
            >
              {previewLabels[key]}
            </button>
          ))}
        </div>
        <pre className="text-xs font-mono bg-[var(--color-bg-tertiary)] p-4 rounded overflow-x-auto text-[var(--color-text)] max-h-80 overflow-y-auto">
          {previewContent[previewTab] || 'Loading...'}
        </pre>
      </div>
    </div>
  )
}
