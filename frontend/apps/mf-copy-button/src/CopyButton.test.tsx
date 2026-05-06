import { act, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CopyButton } from './CopyButton.js';

interface FakeClipboard {
  writeText: ReturnType<typeof vi.fn>;
}

const originalClipboardDescriptor = Object.getOwnPropertyDescriptor(
  globalThis.navigator,
  'clipboard',
);

/**
 * Override the clipboard with a test fake.
 *
 * Must be called AFTER `userEvent.setup()` because user-event v14 unconditionally
 * installs its own clipboard stub on `navigator` during setup, replacing whatever
 * was there before.
 */
const stubClipboard = (clipboard: FakeClipboard) => {
  Object.defineProperty(globalThis.navigator, 'clipboard', {
    value: clipboard,
    configurable: true,
    writable: true,
  });
};

afterEach(() => {
  if (originalClipboardDescriptor) {
    Object.defineProperty(globalThis.navigator, 'clipboard', originalClipboardDescriptor);
  } else {
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: undefined,
      configurable: true,
      writable: true,
    });
  }
  vi.useRealTimers();
});

describe('<CopyButton>', () => {
  it('renders the default "Copy" label', () => {
    stubClipboard({ writeText: vi.fn().mockResolvedValue(undefined) });
    render(<CopyButton text="hello" />);
    expect(screen.getByRole('button', { name: /copy/i })).toHaveTextContent('Copy');
  });

  it('calls navigator.clipboard.writeText with the text prop on click', async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard({ writeText });

    render(<CopyButton text="payload" />);
    await user.click(screen.getByRole('button', { name: /copy/i }));

    expect(writeText).toHaveBeenCalledWith('payload');
  });

  it('shows "Copied!" after a successful copy and reverts after resetMs', async () => {
    vi.useFakeTimers();
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard({ writeText });

    render(<CopyButton text="payload" resetMs={1500} />);

    // Use fireEvent so we don't depend on user-event's internal timer machinery
    // while fake timers are active.
    await act(async () => {
      fireEvent.click(screen.getByRole('button'));
    });

    expect(screen.getByRole('button')).toHaveTextContent('Copied!');

    await act(async () => {
      vi.advanceTimersByTime(1500);
    });

    expect(screen.getByRole('button')).toHaveTextContent('Copy');
  });

  it('disables the button while the clipboard promise is pending', async () => {
    const user = userEvent.setup();
    let resolveWrite: (() => void) | undefined;
    const writeText = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveWrite = resolve;
        }),
    );
    stubClipboard({ writeText });

    render(<CopyButton text="payload" />);
    const button = screen.getByRole('button');
    await user.click(button);

    expect(button).toBeDisabled();

    await act(async () => {
      resolveWrite?.();
    });

    expect(button).not.toBeDisabled();
  });

  it('shows an error and does NOT swap to "Copied!" when the clipboard rejects', async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockRejectedValue(new Error('denied'));
    stubClipboard({ writeText });

    render(<CopyButton text="payload" />);
    await user.click(screen.getByRole('button'));

    expect(await screen.findByRole('alert')).toHaveTextContent(/denied/i);
    expect(screen.getByRole('button')).toHaveTextContent('Copy');
    expect(screen.getByRole('button')).not.toHaveTextContent('Copied!');
  });

  it('disables the button when text is empty', async () => {
    const user = userEvent.setup();
    const writeText = vi.fn();
    stubClipboard({ writeText });

    render(<CopyButton text="" />);
    const button = screen.getByRole('button');
    expect(button).toBeDisabled();

    await user.click(button);
    expect(writeText).not.toHaveBeenCalled();
  });

  it('cleans up its reset timer on unmount', async () => {
    vi.useFakeTimers();
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard({ writeText });

    const { unmount } = render(<CopyButton text="payload" resetMs={1500} />);

    await act(async () => {
      fireEvent.click(screen.getByRole('button'));
    });

    // Unmount before the reset timer fires; advancing timers must not throw.
    expect(() => {
      unmount();
      vi.advanceTimersByTime(5000);
    }).not.toThrow();
  });
});
