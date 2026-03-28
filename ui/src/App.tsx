import { Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/Layout'
import Overview from './pages/Overview'
import Analytics from './pages/Analytics'
import Plugins from './pages/Plugins'
import Discovery from './pages/Discovery'
import RateLimits from './pages/RateLimits'
import Identity from './pages/Identity'
import Payments from './pages/Payments'
import Settings from './pages/Settings'
import Logs from './pages/Logs'

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Overview />} />
        <Route path="analytics" element={<Analytics />} />
        <Route path="plugins" element={<Plugins />} />
        <Route path="discovery" element={<Discovery />} />
        <Route path="rate-limits" element={<RateLimits />} />
        <Route path="identity" element={<Identity />} />
        <Route path="payments" element={<Payments />} />
        <Route path="settings" element={<Settings />} />
        <Route path="logs" element={<Logs />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
