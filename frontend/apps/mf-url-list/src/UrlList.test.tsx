import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { UrlList } from './UrlList.js';

interface FakeShortener {
  get: ReturnType<typeof vi.fn>;
}

const renderWith = (
  shortener: FakeShortener,
  props: { codes: readonly string[]; onSelect?: (code: string) => void } = { codes: [] },
) => {
  // biome-ignore lint/suspicious/noExplicitAny: test fake; mirrors createClient shape
  const clients = { shortener: shortener as any };
  if (props.onSelect) {
    return render(<UrlList codes={props.codes} onSelect={props.onSelect} clients={clients} />);
  }
  return render(<UrlList codes={props.codes} clients={clients} />);
};

const makeUrl = (code: string, longUrl: string) => ({
  url: {
    code,
    longUrl,
    createdAt: { seconds: BigInt(1_700_000_000), nanos: 0 },
  },
});

describe('<UrlList>', () => {
  it('renders an empty-state message when codes is empty', () => {
    renderWith({ get: vi.fn() }, { codes: [] });
    expect(screen.getByText(/no urls yet/i)).toBeInTheDocument();
  });

  it('renders one row per code with the code text visible', () => {
    const get = vi.fn().mockReturnValue(new Promise(() => {}));
    renderWith({ get }, { codes: ['abc', 'def'] });

    const rows = screen.getAllByTestId('url-row');
    expect(rows).toHaveLength(2);
    expect(within(rows[0] as HTMLElement).getByText('abc')).toBeInTheDocument();
    expect(within(rows[1] as HTMLElement).getByText('def')).toBeInTheDocument();
  });

  it('shows the long URL on each row after Get resolves', async () => {
    const get = vi.fn().mockImplementation(({ code }: { code: string }) => {
      if (code === 'abc') return Promise.resolve(makeUrl('abc', 'https://example.com/abc'));
      return Promise.resolve(makeUrl('def', 'https://example.com/def'));
    });
    renderWith({ get }, { codes: ['abc', 'def'] });

    expect(await screen.findByText('https://example.com/abc')).toBeInTheDocument();
    expect(await screen.findByText('https://example.com/def')).toBeInTheDocument();
  });

  it('shows an error on a failing row but other rows still resolve', async () => {
    const get = vi.fn().mockImplementation(({ code }: { code: string }) => {
      if (code === 'bad') return Promise.reject(new Error('not found'));
      return Promise.resolve(makeUrl(code, `https://example.com/${code}`));
    });
    renderWith({ get }, { codes: ['bad', 'good'] });

    const status = await screen.findByRole('status');
    expect(status).toHaveTextContent(/not found/i);
    expect(await screen.findByText('https://example.com/good')).toBeInTheDocument();
  });

  it('calls onSelect with the code when a row is clicked', async () => {
    const get = vi.fn().mockResolvedValue(makeUrl('abc', 'https://example.com/abc'));
    const onSelect = vi.fn();
    renderWith({ get }, { codes: ['abc'], onSelect });

    await screen.findByText('https://example.com/abc');
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /abc/i }));

    expect(onSelect).toHaveBeenCalledWith('abc');
  });

  it('refetches when codes prop changes', async () => {
    const get = vi
      .fn()
      .mockImplementation(({ code }: { code: string }) =>
        Promise.resolve(makeUrl(code, `https://example.com/${code}`)),
      );
    const { rerender } = render(
      <UrlList
        codes={['abc']}
        // biome-ignore lint/suspicious/noExplicitAny: test fake; mirrors createClient shape
        clients={{ shortener: { get } as any }}
      />,
    );

    await waitFor(() => expect(get).toHaveBeenCalledWith({ code: 'abc' }));

    rerender(
      <UrlList
        codes={['abc', 'xyz']}
        // biome-ignore lint/suspicious/noExplicitAny: test fake; mirrors createClient shape
        clients={{ shortener: { get } as any }}
      />,
    );

    await waitFor(() => expect(get).toHaveBeenCalledWith({ code: 'xyz' }));
  });
});
