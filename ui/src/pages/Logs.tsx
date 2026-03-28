import { useEffect, useRef, useState } from 'react'
import { connectLogStream } from '../lib/ws'

interface LogEntry {
  timestamp?: string
  method?: string
  path?: string
  status?: number
  duration_ms?: number
  agent?: string
  [key: string]: unknown
}

export default function Logs() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [connected, setConnected] = useState(false)
  const endRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setConnected(true)
    const disconnect = connectLogStream((entry) => {
      setLogs((prev) => [...prev.slice(-499), entry as LogEntry])
    })
    return () => {
      disconnect()
      setConnected(false)
    }
  }, [])

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  const statusColor = (s?: number) => {
    if (!s) return 'text-[var(--color-text-secondary)]'
    if (s < 300) return 'text-green-500'
    if (s < 400) return 'text-blue-500'
    if (s < 500) return 'text-[var(--color-warning)]'
    return 'text-[var(--color-error)]'
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-[var(--color-text)]">Live Logs</h2>
        <div className="flex items-center gap-2 text-sm text-[var(--color-text-secondary)]">
          <span
            className={`inline-block w-2 h-2 rounded-full ${
              connected ? 'bg-green-500' : 'bg-red-500'
            }`}
          />
          {connected ? 'Connected' : 'Disconnected'}
        </div>
      </div>

      <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] overflow-hidden">
        <div className="overflow-y-auto max-h-[calc(100vh-220px)] font-mono text-xs">
          {logs.length === 0 ? (
            <div className="p-8 text-center text-[var(--color-text-secondary)]">
              Waiting for requests...
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
                {logs.map((log, i) => (
                  <tr
                    key={i}
                    className="border-t border-[var(--color-border)] hover:bg-[var(--color-bg-secondary)]"
                  >
                    <td className="px-3 py-1.5 text-[var(--color-text-secondary)]">
                      {log.timestamp ? new Date(log.timestamp).toLocaleTimeString() : '—'}
                    </td>
                    <td className="px-3 py-1.5 text-[var(--color-text)]">{log.method ?? '—'}</td>
                    <td className="px-3 py-1.5 text-[var(--color-text)]">{log.path ?? '—'}</td>
                    <td className={`px-3 py-1.5 font-medium ${statusColor(log.status)}`}>
                      {log.status ?? '—'}
                    </td>
                    <td className="px-3 py-1.5 text-[var(--color-text-secondary)]">
                      {log.duration_ms != null ? `${log.duration_ms}ms` : '—'}
                    </td>
                    <td className="px-3 py-1.5 text-[var(--color-text-secondary)]">
                      {log.agent ?? '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <div ref={endRef} />
        </div>
      </div>
    </div>
  )
}
