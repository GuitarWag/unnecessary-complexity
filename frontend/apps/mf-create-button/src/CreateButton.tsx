import type { ReactNode } from 'react';
import { useState } from 'react';

export interface CreateButtonProps {
  onClick?: () => void | Promise<void>;
  disabled?: boolean;
  /** Shown while an async onClick is pending. */
  busyLabel?: string;
  children?: ReactNode;
  variant?: 'primary' | 'secondary';
}

export function CreateButton({
  onClick,
  disabled = false,
  busyLabel = 'Creating…',
  children,
  variant = 'primary',
}: CreateButtonProps) {
  const [busy, setBusy] = useState(false);

  const handleClick = () => {
    if (!onClick) return;
    let result: void | Promise<void>;
    try {
      result = onClick();
    } catch (err) {
      console.error('CreateButton onClick threw synchronously', err);
      return;
    }
    if (isPromise(result)) {
      setBusy(true);
      result
        .catch((err: unknown) => {
          console.error('CreateButton onClick rejected', err);
        })
        .finally(() => {
          setBusy(false);
        });
    }
  };

  const className = `cb cb--${variant}`;
  const label = busy ? busyLabel : (children ?? 'Create');

  return (
    <button type="button" className={className} disabled={disabled || busy} onClick={handleClick}>
      {label}
    </button>
  );
}

function isPromise(value: unknown): value is Promise<void> {
  return (
    typeof value === 'object' &&
    value !== null &&
    'then' in value &&
    typeof (value as { then: unknown }).then === 'function'
  );
}
