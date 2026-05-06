import { Suspense, lazy, useEffect, useState } from 'react';

const RemoteUrlInput = lazy(() =>
  import('mfUrlInput/UrlInput').then((m) => ({ default: m.UrlInput })),
);
const RemoteUrlList = lazy(() => import('mfUrlList/UrlList').then((m) => ({ default: m.UrlList })));

type WindowWithPush = Window & { __pushCode__?: (code: string) => void };

export function HomePage() {
  const [codes, setCodes] = useState<string[]>([]);

  useEffect(() => {
    const w = window as WindowWithPush;
    w.__pushCode__ = (code) => {
      setCodes((prev) => (prev.includes(code) ? prev : [code, ...prev].slice(0, 20)));
    };
    return () => {
      w.__pushCode__ = undefined;
    };
  }, []);

  return (
    <section className="page">
      <header className="hero">
        <h2>Shorten any URL</h2>
        <p className="hero__subtitle">
          Paste a long link, get a short one. Tracks clicks, exposes analytics, falls over with
          taste.
        </p>
      </header>

      <Suspense fallback={<p className="placeholder">Loading input…</p>}>
        <RemoteUrlInput />
      </Suspense>

      <section className="recent">
        <h3>Recent</h3>
        <Suspense fallback={<p className="placeholder">Loading list…</p>}>
          <RemoteUrlList codes={codes} />
        </Suspense>
      </section>
    </section>
  );
}
