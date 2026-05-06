import { type Clients, createClients } from '@url-shortener/proto-web';
import { useEffect, useMemo, useState } from 'react';
import './AnalyticsChart.css';

export interface AnalyticsChartProps {
  code: string;
  /** Inclusive ISO date string YYYY-MM-DD; passed through to API. */
  fromDate?: string;
  /** Inclusive ISO date string YYYY-MM-DD; passed through to API. */
  toDate?: string;
  /** Gateway base URL. Ignored when clients is provided. */
  baseUrl?: string;
  /** Pre-built clients (used by tests and the shell to share connections). */
  clients?: Pick<Clients, 'analytics'>;
}

interface DailyPoint {
  date: string;
  count: number;
}

interface StatsView {
  total: number;
  lastClickedAt: Date | null;
  daily: DailyPoint[];
}

const BAR_WIDTH = 24;
const BAR_GAP = 4;
const CHART_HEIGHT = 80;

export function AnalyticsChart({
  code,
  fromDate,
  toDate,
  baseUrl = 'http://localhost:8080',
  clients,
}: AnalyticsChartProps) {
  const c = useMemo(() => clients ?? createClients({ baseUrl }), [baseUrl, clients]);

  const [data, setData] = useState<StatsView | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!code) {
      setData(null);
      setError(null);
      setLoading(false);
      return;
    }

    let cancelled = false;
    setLoading(true);
    setError(null);
    setData(null);

    c.analytics
      .stats({ code, fromDate: fromDate ?? '', toDate: toDate ?? '' })
      .then((resp) => {
        if (cancelled) return;
        setData(toView(resp));
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err : new Error('Unknown error'));
        setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [c, code, fromDate, toDate]);

  if (!code) {
    return <p>Provide a code.</p>;
  }

  if (loading) {
    // biome-ignore lint/a11y/useSemanticElements: spec requires <p role="status">
    return <p role="status">Loading…</p>;
  }

  if (error) {
    return <p role="alert">{error.message}</p>;
  }

  if (!data || data.total === 0) {
    return <p>No clicks yet.</p>;
  }

  const maxCount = data.daily.reduce((m, d) => (d.count > m ? d.count : m), 0);
  const chartWidth = Math.max(
    BAR_WIDTH,
    data.daily.length * BAR_WIDTH + Math.max(0, data.daily.length - 1) * BAR_GAP,
  );

  return (
    <section className="analytics-chart">
      <h3>{data.total} clicks</h3>
      {data.lastClickedAt && <p>Last clicked: {data.lastClickedAt.toLocaleString()}</p>}
      <svg
        role="img"
        aria-label="Daily clicks"
        width={chartWidth}
        height={CHART_HEIGHT}
        viewBox={`0 0 ${chartWidth} ${CHART_HEIGHT}`}
      >
        {data.daily.map((d, i) => {
          const h = maxCount === 0 ? 0 : Math.round((d.count / maxCount) * CHART_HEIGHT);
          const x = i * (BAR_WIDTH + BAR_GAP);
          const y = CHART_HEIGHT - h;
          return (
            <rect key={d.date} x={x} y={y} width={BAR_WIDTH} height={h}>
              <title>
                {d.date}: {d.count}
              </title>
            </rect>
          );
        })}
      </svg>
      <table>
        <thead>
          <tr>
            <th scope="col">Date</th>
            <th scope="col">Count</th>
          </tr>
        </thead>
        <tbody>
          {data.daily.map((d) => (
            <tr key={d.date} data-testid="daily-row">
              <td>{d.date}</td>
              <td>{d.count}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

interface RawTimestamp {
  seconds?: bigint | number;
  nanos?: number;
  toDate?: () => Date;
}

interface RawDaily {
  date: string;
  count: bigint | number;
}

interface RawStats {
  total: bigint | number;
  lastClickedAt?: RawTimestamp | undefined;
  daily?: RawDaily[];
}

function toView(resp: unknown): StatsView {
  const r = resp as RawStats;
  return {
    total: toNumber(r.total),
    lastClickedAt: timestampToDate(r.lastClickedAt),
    daily: (r.daily ?? []).map((d) => ({ date: d.date, count: toNumber(d.count) })),
  };
}

function toNumber(v: bigint | number | undefined): number {
  if (typeof v === 'bigint') return Number(v);
  if (typeof v === 'number') return v;
  return 0;
}

function timestampToDate(t: RawTimestamp | undefined): Date | null {
  if (!t) return null;
  if (typeof t.toDate === 'function') return t.toDate();
  if (t.seconds !== undefined) {
    const seconds = typeof t.seconds === 'bigint' ? Number(t.seconds) : t.seconds;
    const nanos = t.nanos ?? 0;
    return new Date(seconds * 1000 + Math.floor(nanos / 1_000_000));
  }
  return null;
}
