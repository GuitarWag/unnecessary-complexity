import { Suspense, lazy, useState } from 'react';

const RemoteAnalyticsChart = lazy(() =>
  import('mfAnalyticsChart/AnalyticsChart').then((m) => ({ default: m.AnalyticsChart })),
);

export function StatsPage() {
  const [code, setCode] = useState('');
  const [submitted, setSubmitted] = useState('');
  const [refresh, setRefresh] = useState(0);

  return (
    <section className="page">
      <header className="hero">
        <h2>Click analytics</h2>
        <p className="hero__subtitle">Type a short code to see its click history.</p>
      </header>
      <form
        className="stats-form card"
        onSubmit={(e) => {
          e.preventDefault();
          setSubmitted(code.trim());
          setRefresh((n) => n + 1);
        }}
      >
        <label htmlFor="stats-code">Short code</label>
        <div className="stats-form__row">
          <input
            id="stats-code"
            type="text"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder="abc12345"
          />
          <button type="submit" disabled={code.trim() === ''}>
            Show stats
          </button>
        </div>
      </form>

      {submitted && (
        <Suspense fallback={<p className="placeholder">Loading chart…</p>}>
          <RemoteAnalyticsChart key={`${submitted}-${refresh}`} code={submitted} />
        </Suspense>
      )}
    </section>
  );
}
