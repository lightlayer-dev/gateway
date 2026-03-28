import { useState, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  fetchConfig,
  fetchStatus,
  updateConfig,
  exportConfig,
  importConfig,
} from '../lib/api'
import { Save, Download, Upload, Loader2 } from 'lucide-react'

export default function Settings() {
  const queryClient = useQueryClient()
  const { data: config } = useQuery({ queryKey: ['config'], queryFn: fetchConfig })
  const { data: status } = useQuery({ queryKey: ['status'], queryFn: fetchStatus })

  const [originUrl, setOriginUrl] = useState<string | null>(null)
  const [listenPort, setListenPort] = useState<string | null>(null)
  const [adminPort, setAdminPort] = useState<string | null>(null)
  const [tlsEnabled, setTlsEnabled] = useState<boolean | null>(null)
  const [tlsCert, setTlsCert] = useState<string | null>(null)
  const [tlsKey, setTlsKey] = useState<string | null>(null)
  const [importYaml, setImportYaml] = useState('')
  const [showImport, setShowImport] = useState(false)
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const effectiveOrigin = originUrl ?? config?.gateway?.origin?.url ?? ''
  const effectiveListen = listenPort ?? String(config?.gateway?.listen?.port ?? '')
  const effectiveAdmin = adminPort ?? String(config?.admin?.port ?? '')
  const effectiveTls = tlsEnabled ?? !!(config?.gateway?.listen?.tls?.cert)
  const effectiveCert = tlsCert ?? config?.gateway?.listen?.tls?.cert ?? ''
  const effectiveKey = tlsKey ?? config?.gateway?.listen?.tls?.key ?? ''

  const isDirty =
    originUrl !== null ||
    listenPort !== null ||
    adminPort !== null ||
    tlsEnabled !== null ||
    tlsCert !== null ||
    tlsKey !== null

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!config) throw new Error('No config loaded')
      const updated = {
        ...config,
        gateway: {
          ...config.gateway,
          origin: { ...config.gateway.origin, url: effectiveOrigin },
          listen: {
            ...config.gateway.listen,
            port: Number(effectiveListen) || config.gateway.listen.port,
            ...(effectiveTls
              ? { tls: { cert: effectiveCert, key: effectiveKey } }
              : {}),
          },
        },
        admin: { ...config.admin, port: Number(effectiveAdmin) || config.admin.port },
      }
      // Remove tls key entirely if disabled.
      if (!effectiveTls) {
        delete (updated.gateway.listen as Record<string, unknown>).tls
      }
      return updateConfig(updated)
    },
    onSuccess: () => {
      setMessage({ type: 'success', text: 'Settings saved and config reloaded.' })
      setOriginUrl(null)
      setListenPort(null)
      setAdminPort(null)
      setTlsEnabled(null)
      setTlsCert(null)
      setTlsKey(null)
      queryClient.invalidateQueries({ queryKey: ['config'] })
      queryClient.invalidateQueries({ queryKey: ['status'] })
    },
    onError: (err) => {
      setMessage({ type: 'error', text: `Save failed: ${err.message}` })
    },
  })

  const importMutation = useMutation({
    mutationFn: () => importConfig(importYaml),
    onSuccess: () => {
      setMessage({ type: 'success', text: 'Config imported and reloaded.' })
      setImportYaml('')
      setShowImport(false)
      queryClient.invalidateQueries({ queryKey: ['config'] })
      queryClient.invalidateQueries({ queryKey: ['status'] })
    },
    onError: (err) => {
      setMessage({ type: 'error', text: `Import failed: ${err.message}` })
    },
  })

  const handleExport = async () => {
    try {
      const yaml = await exportConfig()
      const blob = new Blob([yaml], { type: 'application/x-yaml' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'gateway.yaml'
      a.click()
      URL.revokeObjectURL(url)
    } catch {
      setMessage({ type: 'error', text: 'Export failed' })
    }
  }

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = () => {
      setImportYaml(reader.result as string)
      setShowImport(true)
    }
    reader.readAsText(file)
    e.target.value = ''
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-[var(--color-text)]">Settings</h2>
        {isDirty && (
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
            Save
          </button>
        )}
      </div>

      {message && (
        <div
          className={`text-sm rounded-lg p-3 ${
            message.type === 'success'
              ? 'bg-green-500/10 border border-green-500/20 text-green-600 dark:text-green-400'
              : 'bg-red-500/10 border border-red-500/20 text-red-600 dark:text-red-400'
          }`}
        >
          {message.text}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Gateway settings */}
        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Gateway</h3>

          <div className="space-y-1">
            <label className="text-xs text-[var(--color-text-secondary)]">Origin URL</label>
            <input
              type="text"
              value={effectiveOrigin}
              onChange={(e) => setOriginUrl(e.target.value)}
              className="w-full px-3 py-2 text-sm font-mono bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-xs text-[var(--color-text-secondary)]">Listen Port</label>
              <input
                type="text"
                value={effectiveListen}
                onChange={(e) => setListenPort(e.target.value)}
                className="w-full px-3 py-2 text-sm font-mono bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs text-[var(--color-text-secondary)]">Admin Port</label>
              <input
                type="text"
                value={effectiveAdmin}
                onChange={(e) => setAdminPort(e.target.value)}
                className="w-full px-3 py-2 text-sm font-mono bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
              />
            </div>
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <label className="text-xs text-[var(--color-text-secondary)]">TLS</label>
              <button
                onClick={() => setTlsEnabled(!effectiveTls)}
                className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors ${
                  effectiveTls ? 'bg-[var(--color-accent)]' : 'bg-gray-400'
                }`}
              >
                <span
                  className={`inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform ${
                    effectiveTls ? 'translate-x-4.5' : 'translate-x-0.5'
                  }`}
                />
              </button>
            </div>

            {effectiveTls && (
              <div className="space-y-2">
                <div className="space-y-1">
                  <label className="text-xs text-[var(--color-text-secondary)]">Cert Path</label>
                  <input
                    type="text"
                    value={effectiveCert}
                    onChange={(e) => setTlsCert(e.target.value)}
                    placeholder="/path/to/cert.pem"
                    className="w-full px-3 py-2 text-sm font-mono bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-[var(--color-text-secondary)]">Key Path</label>
                  <input
                    type="text"
                    value={effectiveKey}
                    onChange={(e) => setTlsKey(e.target.value)}
                    placeholder="/path/to/key.pem"
                    className="w-full px-3 py-2 text-sm font-mono bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
                  />
                </div>
              </div>
            )}
          </div>
        </div>

        {/* Runtime info */}
        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-4">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Runtime</h3>
          <div className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-[var(--color-text-secondary)]">Version</span>
              <span className="text-[var(--color-text)]">{status?.version ?? '—'}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-[var(--color-text-secondary)]">Uptime</span>
              <span className="text-[var(--color-text)]">{status?.uptime ?? '—'}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-[var(--color-text-secondary)]">Status</span>
              <span className="text-green-500">{status?.status ?? '—'}</span>
            </div>
          </div>

          <hr className="border-[var(--color-border)]" />

          <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Config Import / Export</h3>
          <div className="flex gap-2">
            <button
              onClick={handleExport}
              className="flex items-center gap-2 px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] hover:bg-[var(--color-bg-secondary)] transition-colors"
            >
              <Download size={14} />
              Export YAML
            </button>
            <button
              onClick={() => fileInputRef.current?.click()}
              className="flex items-center gap-2 px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] hover:bg-[var(--color-bg-secondary)] transition-colors"
            >
              <Upload size={14} />
              Upload YAML
            </button>
            <input
              ref={fileInputRef}
              type="file"
              accept=".yaml,.yml"
              onChange={handleFileUpload}
              className="hidden"
            />
            <button
              onClick={() => setShowImport(!showImport)}
              className="px-3 py-2 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] hover:bg-[var(--color-bg-secondary)] transition-colors"
            >
              Paste YAML
            </button>
          </div>
        </div>
      </div>

      {/* Import YAML textarea */}
      {showImport && (
        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5 space-y-3">
          <h3 className="text-sm font-medium text-[var(--color-text-secondary)]">Import YAML Config</h3>
          <textarea
            value={importYaml}
            onChange={(e) => setImportYaml(e.target.value)}
            placeholder="Paste your gateway.yaml content here..."
            rows={12}
            className="w-full px-3 py-2 text-xs font-mono bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)] resize-y"
          />
          <div className="flex gap-2">
            <button
              onClick={() => importMutation.mutate()}
              disabled={!importYaml.trim() || importMutation.isPending}
              className="flex items-center gap-2 px-4 py-2 bg-[var(--color-accent)] text-white text-sm rounded-lg hover:opacity-90 disabled:opacity-50 transition-opacity"
            >
              {importMutation.isPending ? (
                <Loader2 size={14} className="animate-spin" />
              ) : (
                <Upload size={14} />
              )}
              Import & Reload
            </button>
            <button
              onClick={() => {
                setShowImport(false)
                setImportYaml('')
              }}
              className="px-4 py-2 text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text)]"
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Full config display */}
      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
        <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-3">
          Current Configuration
        </h3>
        {config ? (
          <pre className="text-xs font-mono bg-[var(--color-bg-tertiary)] p-4 rounded overflow-x-auto text-[var(--color-text)]">
            {JSON.stringify(config, null, 2)}
          </pre>
        ) : (
          <p className="text-sm text-[var(--color-text-secondary)]">Loading...</p>
        )}
      </div>
    </div>
  )
}
