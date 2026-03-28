import { NavLink } from 'react-router-dom'
import {
  LayoutDashboard,
  BarChart3,
  Puzzle,
  Globe,
  Shield,
  CreditCard,
  Gauge,
  Settings,
  ScrollText,
} from 'lucide-react'

const nav = [
  { to: '/', icon: LayoutDashboard, label: 'Overview' },
  { to: '/analytics', icon: BarChart3, label: 'Analytics' },
  { to: '/plugins', icon: Puzzle, label: 'Plugins' },
  { to: '/discovery', icon: Globe, label: 'Discovery' },
  { to: '/rate-limits', icon: Gauge, label: 'Rate Limits' },
  { to: '/identity', icon: Shield, label: 'Identity' },
  { to: '/payments', icon: CreditCard, label: 'Payments' },
  { to: '/settings', icon: Settings, label: 'Settings' },
  { to: '/logs', icon: ScrollText, label: 'Logs' },
]

export default function Sidebar() {
  return (
    <aside className="w-56 shrink-0 bg-[var(--color-sidebar-bg)] text-[var(--color-sidebar-text)] flex flex-col h-screen sticky top-0">
      <div className="px-5 py-5 border-b border-white/10">
        <span className="text-lg font-semibold text-white tracking-tight">
          ⚡ LightLayer
        </span>
      </div>
      <nav className="flex-1 py-3 overflow-y-auto">
        {nav.map(({ to, icon: Icon, label }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            className={({ isActive }) =>
              `flex items-center gap-3 px-5 py-2.5 text-sm transition-colors ${
                isActive
                  ? 'text-[var(--color-sidebar-active)] bg-white/5 border-r-2 border-[var(--color-sidebar-active)]'
                  : 'hover:bg-[var(--color-sidebar-hover)] hover:text-white'
              }`
            }
          >
            <Icon size={18} />
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="px-5 py-4 border-t border-white/10 text-xs text-[var(--color-sidebar-text)]/60">
        Gateway Dashboard
      </div>
    </aside>
  )
}
