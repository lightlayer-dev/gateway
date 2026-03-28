import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from 'recharts'

interface DataPoint {
  time: string
  value: number
}

interface Props {
  data: DataPoint[]
  title: string
}

export default function Chart({ data, title }: Props) {
  return (
    <div className="bg-[var(--color-bg)] rounded-lg border border-[var(--color-border)] p-5">
      <h3 className="text-sm font-medium text-[var(--color-text-secondary)] mb-4">{title}</h3>
      <ResponsiveContainer width="100%" height={240}>
        <LineChart data={data}>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 12, fill: 'var(--color-text-secondary)' }}
            stroke="var(--color-border)"
          />
          <YAxis
            tick={{ fontSize: 12, fill: 'var(--color-text-secondary)' }}
            stroke="var(--color-border)"
          />
          <Tooltip
            contentStyle={{
              background: 'var(--color-bg)',
              border: '1px solid var(--color-border)',
              borderRadius: 6,
              fontSize: 12,
            }}
          />
          <Line
            type="monotone"
            dataKey="value"
            stroke="var(--color-accent)"
            strokeWidth={2}
            dot={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
