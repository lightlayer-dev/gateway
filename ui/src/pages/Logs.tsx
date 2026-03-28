import { useCallback, useEffect, useRef, useState } from 'react'
import { connectLogStream } from '../lib/ws'
import { Pause, Play, Trash2, Filter } from 'lucide-react'

interface LogEntry {
  timestamp?: string
  method?: string
  path?: string
  status?: number
  status_code?: number
  duration_ms?: number
  agent?: string
  [key: string]: unknown
}

export default function Logs() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [connected, setConnected] = useState(false)
  const [paused, setPaused] = useState(false)
  const [filterAgent, setFilterAgent] = useState('')
  const [filterStatus, setFilterStatus] = useState('')
  const [filterPath, setFilterPath] = useState('')
  const [showFilters, setShowFilters] = useState(false)
  const endRef = useRef<HTMLDivElement>(null)
  const pausedRef = useRef(false)
  const bufferRef = useRef<LogEntry[]>([])

  pausedRef.current = paused

  const handleLog = useCallback((entry: Record<string, unknown>) => {
    const log = entry as LogEntry
    if (pausedRef.current) {
      bufferRef.current.push(log)
      if (bufferRef.current.length > 500) bufferRef.current.shift()
    } else {
      setLogs((prev) => [...prev.slice(-499), log])
    }
  }, [])

  useEffect(() => {
    setConnected(true)
    const disconnect = connectLogStream(handleLog)
    return () => {
      disconnect()
      setConnected(false)
    }
  }, [handleLog])

  useEffect(() => {
    if (!paused) {
      endRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [logs, paused])

  const handleResume = () => {
    setPaused(false)
    if (bufferRef.current.length > 0) {
      setLogs((prev) => [...prev, ...bufferRef.current].slice(-500))
      bufferRef.current = []
    }
  }

  const getStatus = (log: LogEntry) => log.status ?? log.status_code

  const statusColor = (s?: number) => {
    if (!s) return 'text-[var(--color-text-secondary)]'
    if (s < 300) return 'text-green-500'
    if (s < 400) return 'text-yellow-500'
    if (s < 500) return 'text-orange-500'
    return 'text-red-500'
  }

  const statusBg = (s?: number) => {
    if (!s) return ''
    if (s < 300) return ''
    if (s < 400) return ''
    if (s < 500) return 'bg-orange-500/5'
    return 'bg-red-500/5'
  }

  const filteredLogs = logs.filter((log) => {
    if (filterAgent && !(log.agent ?? '').toLowerCase().includes(filterAgent.toLowerCase())) return false
    if (filterStatus) {
      const s = getStatus(log)
      if (!s) return false
      if (filterStatus === '2xx' && (s < 200 || s >= 300)) return false
      if (filterStatus === '3xx' && (s < 300 || s >= 400)) return false
      if (filterStatus === '4xx' && (s < 400 || s >= 500)) return false
      if (filterStatus === '5xx' && s < 500) return false
    }
    if (filterPath && !(log.path ?? '').toLowerCase().includes(filterPath.toLowerCase())) return false
    return true
  })

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-[var(--color-text)]">Live Logs</h2>
        <div className="flex items-center gap-3">
          <button
            onClick={() => setShowFilters(!showFilters)}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border transition-colors ${
              showFilters || filterAgent || filterStatus || filterPath
                ? 'border-[var(--color-accent)] text-[var(--color-accent)]'
                : 'border-[var(--color-border)] text-[var(--color-text-secondary)] hover:text-[var(--color-text)]'
            }`}
          >
            <Filter size={14} />
            Filters
            {(filterAgent || filterStatus || filterPath) && (
              <span className="w-2 h-2 rounded-full bg-[var(--color-accent)]" />
            )}
          </button>
          <button
            onClick={() => (paused ? handleResume() : setPaused(true))}
            className={`flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border transition-colors ${
              paused
                ? 'border-[var(--color-warning)] text-[var(--color-warning)]'
                : 'border-[var(--color-border)] text-[var(--color-text-secondary)] hover:text-[var(--color-text)]'
            }`}
          >
            {paused ? <Play size={14} /> : <Pause size={14} />}
            {paused ? `Resume (${bufferRef.current.length} buffered)` : 'Pause'}
          </button>
          <button
            onClick={() => setLogs([])}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border border-[var(--color-border)] text-[var(--color-text-secondary)] hover:text-[var(--color-text)] transition-colors"
          >
            <Trash2 size={14} />
            Clear
          </button>
          <div className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
            <span
              className={`inline-block w-2 h-2 rounded-full ${
                connected ? 'bg-green-500' : 'bg-red-500'
              }`}
            />
            {connected ? 'Connected' : 'Disconnected'}
          </div>
        </div>
      </div>

      {showFilters && (
        <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-4 flex gap-3 flex-wrap items-end">
          <div className="space-y-1">
            <label className="text-xs text-[var(--color-text-secondary)]">Agent Name</label>
            <input
              type="text"
              value={filterAgent}
              onChange={(e) => setFilterAgent(e.target.value)}
              placeholder="e.g. ClaudeBot"
              className="px-3 py-1.5 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)] w-40"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-[var(--color-text-secondary)]">Status Code</label>
            <select
              value={filterStatus}
              onChange={(e) => setFilterStatus(e.target.value)}
              className="px-3 py-1.5 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]"
            >
              <option value="">All</option>
              <option value="2xx">2xx Success</option>
              <option value="3xx">3xx Redirect</option>
              <option value="4xx">4xx Client Error</option>
              <option value="5xx">5xx Server Error</option>
            </select>
          </div>
          <div className="space-y-1">
            <label className="text-xs text-[var(--color-text-secondary)]">Path Pattern</label>
            <input
              type="text"
              value={filterPath}
              onChange={(e) => setFilterPath(e.target.value)}
              placeholder="e.g. /api/widgets"
              className="px-3 py-1.5 text-sm bg-[var(--color-bg-tertiary)] border border-[var(--color-border)] rounded-md text-[var(--color-text)] focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)] w-48"
            />
          </div>
          {(filterAgent || filterStatus || filterPath) && (
            <button
              onClick={() => {
                setFilterAgent('')
                setFilterStatus('')
                setFilterPath('')
              }}
              className="px-3 py-1.5 text-sm text-[var(--color-text-secondary)] hover:text-[var(--color-text)]"
            >
              Clear filters
            </button>
          )}
        </div>
      )}

      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] overflow-hidden">
        <div className="overflow-y-auto max-h-[calc(100vh-220px)] font-mono text-xs">
          {filteredLogs.length === 0 ? (
            <div className="p-8 text-center text-[var(--color-text-secondary)]">
              {logs.length > 0 ? 'No logs match current filters' : 'Waiting for requests...'}
            </div>
          ) : (
            <table className="w-full">
              <thead className="sticky top-0 bg-[var(--color-bg-tertiary)]">
                <tr className="text-left text-[var(--color-text-secondary)]">
                  <th className="px-3 py-2">Time</th>
                  <th className="px-3 py-2">Method</th>
                  <th className="px-3 py-2">Path</th>
                  <th className="px-3 py-2">Status</th>
                  <th className="px-3 py-2">Duration</th>
                  <th className="px-3 py-2">Agent</th>
                </tr>
              </thead>
              <tbody>
                {filteredLogs.map((log, i) => {
                  const s = getStatus(log)
                  return (
                    <tr
                      key={i}
                      className={`border-t border-[var(--color-border)] hover:bg-[var(--color-bg-secondary)] ${statusBg(s)}`}
                    >
                      <td className="px-3 py-1.5 text-[var(--color-text-secondary)]">
                        {log.timestamp ? new Date(log.timestamp).toLocaleTimeString() : '—'}
                      </td>
                      <td className="px-3 py-1.5 text-[var(--color-text)]">{log.method ?? '—'}</td>
                      <td className="px-3 py-1.5 text-[var(--color-text)]">{log.path ?? '—'}</td>
                      <td className={`px-3 py-1.5 font-medium ${statusColor(s)}`}>
                        {s ?? '—'}
                      </td>
                      <td className="px-3 py-1.5 text-[var(--color-text-secondary)]">
                        {log.duration_ms != null ? `${log.duration_ms}ms` : '—'}
                      </td>
                      <td className="px-3 py-1.5 text-[var(--color-text-secondary)]">
                        {log.agent ?? '—'}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
          <div ref={endRef} />
        </div>
      </div>
    </div>
  )
}
