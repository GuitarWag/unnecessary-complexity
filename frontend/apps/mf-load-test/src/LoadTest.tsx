import { type Clients, createClients } from '@url-shortener/proto-web';
import { Preset, Status } from '@url-shortener/proto-web/loadgen';
import { useMemo, useRef, useState } from 'react';
import './LoadTest.css';

export interface LoadTestProps {
  /** Gateway base URL. Ignored when clients is provided. */
  baseUrl?: string;
  /** Pre-built clients (used by tests). */
  clients?: Pick<Clients, 'loadgen'>;
}

interface PresetCard {
  preset: Preset;
  name: string;
  scenario: 'read' | 'mixed';
  rps: number;
  durationSeconds: number;
}

const PRESETS: readonly PresetCard[] = [
  { preset: Preset.LOW, name: 'low', scenario: 'read', rps: 100, durationSeconds: 10 },
  { preset: Preset.MEDIUM, name: 'medium', scenario: 'read', rps: 1000, durationSeconds: 15 },
  { preset: Preset.HIGH, name: 'high', scenario: 'read', rps: 5000, durationSeconds: 20 },
  { preset: Preset.XHIGH, name: 'xhigh', scenario: 'read', rps: 10000, durationSeconds: 20 },
  { preset: Preset.XXHIGH, name: 'xxhigh', scenario: 'mixed', rps: 25000, durationSeconds: 20 },
  { preset: Preset.INSANE, name: 'insane', scenario: 'mixed', rps: 50000, durationSeconds: 30 },
];

interface SampleView {
  elapsedSeconds: number;
  currentRps: number;
  totalRequests: number;
  errors: number;
  p50Ms: number;
  p95Ms: number;
  p99Ms: number;
  status: Status;
  message: string;
}

interface RawSample {
  elapsedSeconds?: number;
  currentRps?: number;
  totalRequests?: bigint | number;
  errors?: bigint | number;
  p50Ms?: number;
  p95Ms?: number;
  p99Ms?: number;
  status?: Status;
  message?: string;
}

function toView(s: RawSample): SampleView {
  return {
    elapsedSeconds: s.elapsedSeconds ?? 0,
    currentRps: s.currentRps ?? 0,
    totalRequests: toNumber(s.totalRequests),
    errors: toNumber(s.errors),
    p50Ms: s.p50Ms ?? 0,
    p95Ms: s.p95Ms ?? 0,
    p99Ms: s.p99Ms ?? 0,
    status: s.status ?? Status.STATUS_UNSPECIFIED,
    message: s.message ?? '',
  };
}

function toNumber(v: bigint | number | undefined): number {
  if (typeof v === 'bigint') return Number(v);
  if (typeof v === 'number') return v;
  return 0;
}

type RunState = 'idle' | 'running' | 'completed' | 'failed';

const STATE_TO_PILL_LABEL: Record<RunState, string> = {
  idle: 'idle',
  running: 'running',
  completed: 'completed',
  failed: 'failed',
};

const STATUS_TO_STATE: Partial<Record<Status, RunState>> = {
  [Status.RUNNING]: 'running',
  [Status.COMPLETED]: 'completed',
  [Status.FAILED]: 'failed',
};

export function LoadTest({ baseUrl = 'http://localhost:8080', clients }: LoadTestProps) {
  const c = useMemo(() => clients ?? createClients({ baseUrl }), [baseUrl, clients]);
  const [selected, setSelected] = useState<Preset>(Preset.LOW);
  const [samples, setSamples] = useState<SampleView[]>([]);
  const [state, setState] = useState<RunState>('idle');
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const selectedCard = PRESETS.find((p) => p.preset === selected) ?? PRESETS[0];
  const final = state === 'completed' || state === 'failed' ? samples[samples.length - 1] : null;

  const onRun = async () => {
    if (state === 'running') return;
    setSamples([]);
    setError(null);
    setState('running');

    const ac = new AbortController();
    abortRef.current = ac;
    try {
      const stream = c.loadgen.runLoadTest(
        { preset: selected, scenario: '', targetRps: 0, durationSeconds: 0 },
        { signal: ac.signal },
      );
      for await (const raw of stream) {
        const view = toView(raw as RawSample);
        setSamples((prev) => [...prev, view]);
        const next = STATUS_TO_STATE[view.status];
        if (next === 'completed' || next === 'failed') {
          setState(next);
          if (next === 'failed' && view.message) setError(view.message);
        }
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Unknown error';
      setError(msg);
      setState('failed');
    }
  };

  return (
    <section className="load-test card">
      <header>
        <h2>Load test</h2>
        <p className="hero__subtitle">Pick a preset, hit RUN, watch it bend.</p>
      </header>

      <div className="load-test__presets" role="radiogroup" aria-label="Preset">
        {PRESETS.map((p) => {
          const isSelected = p.preset === selected;
          return (
            <button
              key={p.name}
              type="button"
              // biome-ignore lint/a11y/useSemanticElements: real radio inputs can't host the card layout
              role="radio"
              aria-checked={isSelected}
              data-testid={`preset-${p.name}`}
              className={`load-test__preset${isSelected ? ' load-test__preset--selected' : ''}`}
              onClick={() => setSelected(p.preset)}
            >
              <span className="load-test__preset-name">{p.name}</span>
              <span className="load-test__preset-meta">
                {p.rps.toLocaleString()} RPS · {p.durationSeconds}s · {p.scenario}
              </span>
            </button>
          );
        })}
      </div>

      <div className="load-test__controls">
        <button
          type="button"
          className="primary load-test__run"
          onClick={onRun}
          disabled={state === 'running'}
        >
          {state === 'running' ? 'Running…' : 'RUN'}
        </button>
        <span
          data-testid="status-pill"
          className={`load-test__pill load-test__pill--${state}`}
          aria-live="polite"
        >
          {STATE_TO_PILL_LABEL[state]}
        </span>
        <span className="load-test__preset-meta">
          {selectedCard.name} · {selectedCard.rps.toLocaleString()} RPS · {selectedCard.scenario}
        </span>
      </div>

      {error && (
        <p role="alert" className="url-input__error">
          {error}
        </p>
      )}

      <div className="load-test__charts">
        <Sparkline
          title="current RPS"
          unit=""
          values={samples.map((s) => s.currentRps)}
          format={(n) => n.toLocaleString(undefined, { maximumFractionDigits: 0 })}
        />
        <Sparkline
          title="p99 latency"
          unit="ms"
          values={samples.map((s) => s.p99Ms)}
          format={(n) => n.toFixed(1)}
        />
      </div>

      {final && (
        <div className="load-test__summary" data-testid="summary">
          <SummaryItem label="status" value={STATE_TO_PILL_LABEL[state]} />
          <SummaryItem label="total requests" value={final.totalRequests.toLocaleString()} />
          <SummaryItem label="errors" value={final.errors.toLocaleString()} />
          <SummaryItem label="p50" value={`${final.p50Ms.toFixed(1)} ms`} />
          <SummaryItem label="p95" value={`${final.p95Ms.toFixed(1)} ms`} />
          <SummaryItem label="p99" value={`${final.p99Ms.toFixed(1)} ms`} />
        </div>
      )}
    </section>
  );
}

interface SparklineProps {
  title: string;
  unit: string;
  values: number[];
  format: (n: number) => string;
}

const SPARK_W = 280;
const SPARK_H = 60;

function Sparkline({ title, unit, values, format }: SparklineProps) {
  const last = values.length === 0 ? 0 : values[values.length - 1];
  const max = values.reduce((m, v) => (v > m ? v : m), 0);
  const points =
    values.length === 0
      ? ''
      : values
          .map((v, i) => {
            const x = (i / Math.max(1, values.length - 1)) * SPARK_W;
            const y = max === 0 ? SPARK_H : SPARK_H - (v / max) * SPARK_H;
            return `${x.toFixed(1)},${y.toFixed(1)}`;
          })
          .join(' ');
  return (
    <div className="load-test__chart">
      <div className="load-test__chart-title">{title}</div>
      <div className="load-test__chart-value" data-testid={`spark-${title.replace(/\s+/g, '-')}`}>
        {format(last)} {unit}
      </div>
      <svg
        width={SPARK_W}
        height={SPARK_H}
        viewBox={`0 0 ${SPARK_W} ${SPARK_H}`}
        role="img"
        aria-label={`${title} sparkline`}
      >
        {points && <polyline points={points} fill="none" stroke="#2563eb" strokeWidth="2" />}
      </svg>
    </div>
  );
}

function SummaryItem({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="load-test__summary-label">{label}</div>
      <div className="load-test__summary-value">{value}</div>
    </div>
  );
}
