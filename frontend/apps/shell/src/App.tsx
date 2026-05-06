import { Suspense, lazy } from 'react';
import { BrowserRouter, NavLink, Route, Routes } from 'react-router-dom';
import { ClientsProvider } from './clients.js';

const HomePage = lazy(() => import('./pages/Home.js').then((m) => ({ default: m.HomePage })));
const StatsPage = lazy(() => import('./pages/Stats.js').then((m) => ({ default: m.StatsPage })));

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
          </nav>
        </header>
        <main className="main">
          <Suspense fallback={<p>Loading…</p>}>
            <Routes>
              <Route path="/" element={<HomePage />} />
              <Route path="/stats" element={<StatsPage />} />
            </Routes>
          </Suspense>
        </main>
      </BrowserRouter>
    </ClientsProvider>
  );
}
