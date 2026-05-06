import { useEffect, useRef, useState } from 'react';

export interface CopyButtonProps {
  /** Text to copy to clipboard. */
  text: string;
  /** Idle button label. */
  label?: string;
  /** Label shown after a successful copy. */
  copiedLabel?: string;
  /** How long to show copiedLabel before reverting, in ms. */
  resetMs?: number;
}

type Status = 'idle' | 'pending' | 'copied' | 'error';

export function CopyButton({
  text,
  label = 'Copy',
  copiedLabel = 'Copied!',
  resetMs = 1500,
}: CopyButtonProps) {
  const [status, setStatus] = useState<Status>('idle');
  const [error, setError] = useState<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timerRef.current !== null) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    },
    [],
  );

  const isEmpty = text === '';
  const disabled = isEmpty || status === 'pending';

  const onClick = async () => {
    if (isEmpty) return;

    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }

    setError(null);
    setStatus('pending');
    try {
      await navigator.clipboard.writeText(text);
      setStatus('copied');
      timerRef.current = setTimeout(() => {
        setStatus('idle');
        timerRef.current = null;
      }, resetMs);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Copy failed');
      setStatus('error');
    }
  };

  const buttonLabel = status === 'copied' ? copiedLabel : label;

  return (
    <span className="copy-button">
      <button
        type="button"
        aria-label={label}
        disabled={disabled}
        onClick={onClick}
        className="copy-button__button"
      >
        {buttonLabel}
      </button>
      {error && (
        <span role="alert" className="copy-button__error">
          {error}
        </span>
      )}
    </span>
  );
}
