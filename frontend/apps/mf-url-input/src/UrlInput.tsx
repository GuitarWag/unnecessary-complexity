import { type Clients, createClients } from '@url-shortener/proto-web';
import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react';
import './UrlInput.css';

export interface UrlInputProps {
  /** Gateway base URL. Ignored when clients is provided. */
  baseUrl?: string;
  /** Public-facing base for the displayed short URL (e.g. https://sho.rt). */
  publicBaseUrl?: string;
  /** Pre-built clients (used by tests and the shell to share connections). */
  clients?: Pick<Clients, 'shortener'>;
}

interface ShortResult {
  code: string;
  longUrl: string;
}

declare global {
  interface Window {
    __pushCode__?: (code: string) => void;
  }
}

export function UrlInput({
  baseUrl = 'http://localhost:8080',
  publicBaseUrl,
  clients,
}: UrlInputProps) {
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<ShortResult | null>(null);
  const [copied, setCopied] = useState(false);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const c = useMemo(() => clients ?? createClients({ baseUrl }), [baseUrl, clients]);
  const linkBase = (publicBaseUrl ?? baseUrl).replace(/\/+$/, '');
  const fullUrl = result ? `${linkBase}/${result.code}` : '';

  useEffect(
    () => () => {
      if (copyTimer.current !== null) clearTimeout(copyTimer.current);
    },
    [],
  );

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError(null);
    setResult(null);
    setCopied(false);

    const trimmed = value.trim();
    if (!isValidHttpUrl(trimmed)) {
      setError('URL must use http or https.');
      return;
    }

    setBusy(true);
    try {
      const resp = await c.shortener.shorten({ longUrl: trimmed });
      const url = resp.url;
      if (!url) {
        setError('Server returned no URL.');
        return;
      }
      setResult({ code: url.code, longUrl: url.longUrl });
      window.__pushCode__?.(url.code);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setBusy(false);
    }
  };

  const onCopy = async () => {
    if (!fullUrl) return;
    try {
      await navigator.clipboard.writeText(fullUrl);
      setCopied(true);
      if (copyTimer.current !== null) clearTimeout(copyTimer.current);
      copyTimer.current = setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  };

  return (
    <form onSubmit={onSubmit} className="url-input card">
      <label htmlFor="long-url" className="url-input__label">
        Long URL
      </label>
      <div className="url-input__row">
        <input
          id="long-url"
          type="url"
          inputMode="url"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="https://example.com/very/long/path"
        />
        <button type="submit" disabled={busy || value.trim() === ''}>
          {busy ? 'Shortening…' : 'Shorten'}
        </button>
      </div>

      {error && (
        <p role="alert" className="url-input__error">
          {error}
        </p>
      )}

      {result && (
        <div className="url-input__result">
          <div className="url-input__result-label">Short URL</div>
          <div className="url-input__result-row">
            <a
              href={fullUrl}
              className="url-input__short mono"
              target="_blank"
              rel="noopener noreferrer"
            >
              {fullUrl}
            </a>
            <button
              type="button"
              className="secondary url-input__copy"
              onClick={onCopy}
              aria-label="Copy short URL"
            >
              <CopyIcon />
              <span>{copied ? 'Copied' : 'Copy'}</span>
            </button>
          </div>
          <div className="url-input__result-target mono">→ {result.longUrl}</div>
        </div>
      )}
    </form>
  );
}

function CopyIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

function isValidHttpUrl(s: string): boolean {
  if (!s) return false;
  try {
    const u = new URL(s);
    return u.protocol === 'http:' || u.protocol === 'https:';
  } catch {
    return false;
  }
}
