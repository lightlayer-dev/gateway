const BASE = import.meta.env.VITE_API_BASE ?? ''

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers,
    },
  })
  if (!res.ok) {
    const body = await res.text()
    throw new Error(`API ${res.status}: ${body}`)
  }
  return res.json()
}

// ── Types ────────────────────────────────────────────────────────────────

export interface HealthResponse {
  status: string
  version: string
  uptime: string
}

export interface StatusResponse {
  status: string
  version: string
  uptime: string
  uptime_seconds: number
  origin_url: string
  listen_port: number
  admin_port: number
  total_requests?: number
  unique_agents?: number
  plugins: Array<{ name: string; active: boolean }>
}

export interface Metrics {
  total_requests: number
  unique_agents: number
  avg_latency_ms: number
  error_rate: number
  top_agents: Array<{ agent: string; count: number; last_seen?: string }>
  top_paths: Array<{ path: string; method?: string; count: number; avg_latency_ms?: number }>
  status_distribution: Record<string, number>
  requests_by_hour: Array<{ hour: string; count: number }>
}

export interface AnalyticsResponse {
  events: Array<Record<string, unknown>>
  metrics: Metrics
}

export interface ConfigResponse {
  gateway: {
    listen: { port: number; host: string; tls?: { cert: string; key: string } }
    origin: { url: string; timeout: string }
  }
  plugins: Record<string, { enabled?: boolean; [key: string]: unknown }>
  admin: { enabled: boolean; port: number }
}

export interface Capability {
  name: string
  description: string
  methods: string[]
  paths: string[]
}

export interface DiscoveryPreview {
  well_known_ai: Record<string, unknown>
  agent_card: Record<string, unknown>
  agents_txt: string
  llms_txt: string
}

export interface RateLimitUsageEntry {
  agent: string
  used: number
  limit: number
  window: string
  percent: number
}

export interface RateLimitUsageResponse {
  enabled: boolean
  default?: { requests: number; window: string }
  per_agent?: Record<string, { requests: number; window: string }>
  usage: RateLimitUsageEntry[]
}

export interface AgentRecord {
  name: string
  first_seen: string
  last_seen: string
  total_requests: number
  verified: boolean
}

export interface AgentActivityResponse {
  agents: AgentRecord[]
}

export interface PaymentEvent {
  id: string
  timestamp: string
  agent: string
  method: string
  path: string
  status_code: number
  duration_ms: number
  payment_info: string
}

export interface PaymentHistoryResponse {
  payments: PaymentEvent[]
}

// ── Endpoints ────────────────────────────────────────────────────────────

export const fetchHealth = () => request<HealthResponse>('/api/health')

export const fetchStatus = () => request<StatusResponse>('/api/status')

export const fetchMetrics = (period = '24h') =>
  request<Metrics>(`/api/metrics?period=${period}`)

export const fetchAnalytics = (params?: Record<string, string>) => {
  const qs = params ? '?' + new URLSearchParams(params).toString() : ''
  return request<AnalyticsResponse>(`/api/analytics${qs}`)
}

export const fetchConfig = () => request<ConfigResponse>('/api/config')

export const updateConfig = (body: unknown) =>
  request<{ status: string }>('/api/config', {
    method: 'PUT',
    body: JSON.stringify(body),
  })

export const updatePluginConfig = (body: unknown) =>
  request<{ status: string }>('/api/config/plugins', {
    method: 'PUT',
    body: JSON.stringify(body),
  })

export const exportConfig = async (): Promise<string> => {
  const res = await fetch(`${BASE}/api/config/export`)
  if (!res.ok) throw new Error(`Export failed: ${res.status}`)
  return res.text()
}

export const importConfig = (yaml: string) =>
  request<{ status: string }>('/api/config/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-yaml' },
    body: yaml,
  })

export const fetchAgents = () =>
  request<{ agents: Array<{ name: string; count: number }> }>('/api/agents')

export const fetchDiscoveryPreview = () =>
  request<DiscoveryPreview>('/api/discovery/preview')

export const fetchRateLimitUsage = () =>
  request<RateLimitUsageResponse>('/api/rate-limits/usage')

export const fetchAgentActivity = () =>
  request<AgentActivityResponse>('/api/agents/activity')

export const fetchPaymentHistory = (limit = 50) =>
  request<PaymentHistoryResponse>(`/api/payments/history?limit=${limit}`)

// ── Demo Mode ─────────────────────────────────────────────────────────────

export interface DemoStatusResponse {
  demo_mode: boolean
  demo_api_url: string
}

export const fetchDemoStatus = () =>
  request<DemoStatusResponse>('/api/demo/status')
