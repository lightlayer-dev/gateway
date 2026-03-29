import { useState } from 'react'
import { X } from 'lucide-react'

const DISMISSED_KEY = 'demo-banner-dismissed'

export default function DemoBanner() {
  const [dismissed, setDismissed] = useState(
    () => localStorage.getItem(DISMISSED_KEY) === '1',
  )

  if (dismissed) return null

  return (
    <div className="rounded-lg border border-[var(--color-warning)] bg-[var(--color-warning)]/10 px-4 py-3 flex items-center justify-between">
      <span className="text-sm text-[var(--color-text)]">
        🧪 <strong>Demo Mode</strong> — Exploring with a sample API. Point to
        your own API when ready.
      </span>
      <button
        onClick={() => {
          localStorage.setItem(DISMISSED_KEY, '1')
          setDismissed(true)
        }}
        className="text-[var(--color-text-secondary)] hover:text-[var(--color-text)] ml-4"
        aria-label="Dismiss"
      >
        <X size={16} />
      </button>
    </div>
  )
}
