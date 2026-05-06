import { type Clients, createClients } from '@url-shortener/proto-web';
import { useEffect, useMemo, useState } from 'react';
import './UrlList.css';

export interface UrlListProps {
  /** List of codes to display (caller is responsible for what to track). */
  codes: readonly string[];
  /** Gateway URL when clients is not injected. Default 'http://localhost:8080'. */
  baseUrl?: string;
  /** Pre-built clients (used by tests/shell). */
  clients?: Pick<Clients, 'shortener'>;
  /** Optional callback when a row is clicked. */
  onSelect?: (code: string) => void;
}

interface RowState {
  status: 'loading' | 'ok' | 'error';
  longUrl?: string;
  createdAt?: string;
  error?: string;
}

interface TimestampLike {
  seconds: bigint | number;
  nanos: number;
}

const initialRow = (): RowState => ({ status: 'loading' });

export function UrlList({
  codes,
  baseUrl = 'http://localhost:8080',
  clients,
  onSelect,
}: UrlListProps) {
  const c = useMemo(() => clients ?? createClients({ baseUrl }), [baseUrl, clients]);
  const [rows, setRows] = useState<Record<string, RowState>>({});

  // Stable string identity for the codes list so the effect re-runs on content change
  // without firing on every render when callers pass a fresh array reference.
  const codesKey = codes.join(' ');

  useEffect(() => {
    let cancelled = false;
    const list = codesKey === '' ? [] : codesKey.split(' ');

    setRows(() => {
      const next: Record<string, RowState> = {};
      for (const code of list) {
        next[code] = initialRow();
      }
      return next;
    });

    for (const code of list) {
      c.shortener
        .get({ code })
        .then((resp) => {
          if (cancelled) return;
          const url = resp.url;
          if (!url) {
            setRows((prev) => ({
              ...prev,
              [code]: { status: 'error', error: 'No URL returned.' },
            }));
            return;
          }
          const createdAt = formatTimestamp(url.createdAt);
          setRows((prev) => ({
            ...prev,
            [code]: createdAt
              ? { status: 'ok', longUrl: url.longUrl, createdAt }
              : { status: 'ok', longUrl: url.longUrl },
          }));
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          const message = err instanceof Error ? err.message : 'Unknown error';
          setRows((prev) => ({
            ...prev,
            [code]: { status: 'error', error: message },
          }));
        });
    }

    return () => {
      cancelled = true;
    };
  }, [codesKey, c]);

  if (codes.length === 0) {
    return <p className="url-list__empty">No URLs yet.</p>;
  }

  return (
    <ul className="url-list">
      {codes.map((code) => {
        const row = rows[code] ?? initialRow();
        const content = (
          <>
            <span className="url-list__code">{code}</span>
            {row.status === 'loading' && <span className="url-list__loading">Loading…</span>}
            {row.status === 'ok' && (
              <>
                <span className="url-list__long">{row.longUrl}</span>
                {row.createdAt && <span className="url-list__created">{row.createdAt}</span>}
              </>
            )}
            {row.status === 'error' && <output className="url-list__error">{row.error}</output>}
          </>
        );

        return (
          <li key={code} data-testid="url-row" className="url-list__row">
            {onSelect ? (
              <button type="button" className="url-list__button" onClick={() => onSelect(code)}>
                {content}
              </button>
            ) : (
              content
            )}
          </li>
        );
      })}
    </ul>
  );
}

function formatTimestamp(ts: TimestampLike | undefined): string | undefined {
  if (!ts) return undefined;
  const seconds = typeof ts.seconds === 'bigint' ? Number(ts.seconds) : ts.seconds;
  const millis = seconds * 1000 + Math.floor(ts.nanos / 1_000_000);
  const date = new Date(millis);
  if (Number.isNaN(date.getTime())) return undefined;
  return date.toLocaleString();
}
