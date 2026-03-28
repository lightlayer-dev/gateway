type LogCallback = (entry: Record<string, unknown>) => void

export function connectLogStream(onLog: LogCallback): () => void {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${proto}//${window.location.host}/api/ws/logs`
  let ws: WebSocket | null = null
  let reconnectTimer: ReturnType<typeof setTimeout>
  let stopped = false

  function connect() {
    if (stopped) return
    ws = new WebSocket(url)

    ws.onmessage = (e) => {
      try {
        onLog(JSON.parse(e.data))
      } catch {
        // ignore non-JSON messages
      }
    }

    ws.onclose = () => {
      if (!stopped) {
        reconnectTimer = setTimeout(connect, 3000)
      }
    }

    ws.onerror = () => {
      ws?.close()
    }
  }

  connect()

  return () => {
    stopped = true
    clearTimeout(reconnectTimer)
    ws?.close()
  }
}
