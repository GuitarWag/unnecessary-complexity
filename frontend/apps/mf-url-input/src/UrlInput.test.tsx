import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { UrlInput } from './UrlInput.js';

interface FakeShortener {
  shorten: ReturnType<typeof vi.fn>;
}

const renderWith = (shortener: FakeShortener) =>
  render(
    <UrlInput
      // biome-ignore lint/suspicious/noExplicitAny: test fake; mirrors createClient shape
      clients={{ shortener: shortener as any }}
    />,
  );

describe('<UrlInput>', () => {
  it('disables submit when the input is empty', () => {
    renderWith({ shorten: vi.fn() });
    expect(screen.getByRole('button', { name: /shorten/i })).toBeDisabled();
  });

  it('rejects non-http URLs without calling the API', async () => {
    const shorten = vi.fn();
    renderWith({ shorten });
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/long url/i), 'javascript:alert(1)');
    await user.click(screen.getByRole('button', { name: /shorten/i }));

    expect(shorten).not.toHaveBeenCalled();
    expect(screen.getByRole('alert')).toHaveTextContent(/http or https/i);
  });

  it('shows the short code returned by the API', async () => {
    const shorten = vi.fn().mockResolvedValue({
      url: { code: 'abc12345', longUrl: 'https://example.com' },
      created: true,
    });
    renderWith({ shorten });
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/long url/i), 'https://example.com');
    await user.click(screen.getByRole('button', { name: /shorten/i }));

    expect(await screen.findByText(/abc12345/)).toBeInTheDocument();
    expect(shorten).toHaveBeenCalledWith({ longUrl: 'https://example.com' });
  });

  it('surfaces API errors to the user', async () => {
    const shorten = vi.fn().mockRejectedValue(new Error('network down'));
    renderWith({ shorten });
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/long url/i), 'https://example.com');
    await user.click(screen.getByRole('button', { name: /shorten/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/network down/i);
  });

  it('renders the short URL as a full link based on baseUrl', async () => {
    const shorten = vi.fn().mockResolvedValue({
      url: { code: 'abc123', longUrl: 'https://example.com' },
      created: true,
    });
    render(
      <UrlInput
        baseUrl="https://api.example.com"
        // biome-ignore lint/suspicious/noExplicitAny: test fake
        clients={{ shortener: { shorten } as any }}
      />,
    );
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/long url/i), 'https://example.com');
    await user.click(screen.getByRole('button', { name: /shorten/i }));

    const link = await screen.findByRole('link');
    expect(link).toHaveAttribute('href', 'https://api.example.com/abc123');
    expect(link).toHaveTextContent('https://api.example.com/abc123');
  });

  it('copies the short URL when the copy button is clicked', async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
      writable: true,
    });

    const shorten = vi.fn().mockResolvedValue({
      url: { code: 'cp1', longUrl: 'https://example.com' },
      created: true,
    });
    renderWith({ shorten });

    await user.type(screen.getByLabelText(/long url/i), 'https://example.com');
    await user.click(screen.getByRole('button', { name: /shorten/i }));

    await user.click(await screen.findByRole('button', { name: /copy short url/i }));
    expect(writeText).toHaveBeenCalledWith('http://localhost:8080/cp1');
  });

  it('pushes the new code to window.__pushCode__ on success', async () => {
    const pushed: string[] = [];
    window.__pushCode__ = (code) => {
      pushed.push(code);
    };
    const shorten = vi.fn().mockResolvedValue({
      url: { code: 'pushed1', longUrl: 'https://example.com' },
      created: true,
    });
    renderWith({ shorten });
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/long url/i), 'https://example.com');
    await user.click(screen.getByRole('button', { name: /shorten/i }));

    expect(pushed).toEqual(['pushed1']);
    window.__pushCode__ = undefined;
  });
});
