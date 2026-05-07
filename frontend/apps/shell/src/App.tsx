import { Suspense, lazy } from 'react';
import { BrowserRouter, NavLink, Route, Routes } from 'react-router-dom';
import { ClientsProvider } from './clients.js';

const HomePage = lazy(() => import('./pages/Home.js').then((m) => ({ default: m.HomePage })));
const StatsPage = lazy(() => import('./pages/Stats.js').then((m) => ({ default: m.StatsPage })));
const LoadPage = lazy(() => import('./pages/Load.js').then((m) => ({ default: m.LoadPage })));

declare global {
  interface Window {
    __GATEWAY_URL__?: string;
  }
}

const gatewayBaseUrl = globalThis.window?.__GATEWAY_URL__ ?? 'http://localhost:8080';

export function App() {
  return (
    <ClientsProvider baseUrl={gatewayBaseUrl}>
      <BrowserRouter>
        <header className="topbar">
          <h1>URL Shortener</h1>
          <nav>
            <NavLink to="/" end>
              Shorten
            </NavLink>
            <NavLink to="/stats">Stats</NavLink>
            <NavLink to="/load">Load test</NavLink>
          </nav>
        </header>
        <main className="main">
          <Suspense fallback={<p>Loading…</p>}>
            <Routes>
              <Route path="/" element={<HomePage />} />
              <Route path="/stats" element={<StatsPage />} />
              <Route path="/load" element={<LoadPage />} />
            </Routes>
          </Suspense>
        </main>
      </BrowserRouter>
    </ClientsProvider>
  );
}
