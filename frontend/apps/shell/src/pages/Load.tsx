import { Suspense, lazy } from 'react';

const RemoteLoadTest = lazy(() =>
  import('mfLoadTest/LoadTest').then((m) => ({ default: m.LoadTest })),
);

export function LoadPage() {
  return (
    <section className="page">
      <header className="hero">
        <h2>Load test</h2>
        <p className="hero__subtitle">
          Pick a preset, hit RUN, watch the system bend in real time.
        </p>
      </header>
      <Suspense fallback={<p className="placeholder">Loading load-test panel…</p>}>
        <RemoteLoadTest />
      </Suspense>
    </section>
  );
}
