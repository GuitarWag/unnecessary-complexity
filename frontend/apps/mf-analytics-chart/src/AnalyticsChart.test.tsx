import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { AnalyticsChart } from './AnalyticsChart.js';

interface FakeAnalytics {
  stats: ReturnType<typeof vi.fn>;
}

interface OptionalProps {
  code?: string;
  fromDate?: string;
  toDate?: string;
}

const renderWith = (analytics: FakeAnalytics, props: OptionalProps = {}) => {
  const code = props.code ?? 'abc123';
  return render(
    <AnalyticsChart
      code={code}
      {...(props.fromDate !== undefined ? { fromDate: props.fromDate } : {})}
      {...(props.toDate !== undefined ? { toDate: props.toDate } : {})}
      // biome-ignore lint/suspicious/noExplicitAny: test fake; mirrors createClient shape
      clients={{ analytics: analytics as any }}
    />,
  );
};

const okResponse = (overrides: Record<string, unknown> = {}) => ({
  code: 'abc123',
  total: BigInt(5),
  lastClickedAt: { seconds: BigInt(1_700_000_000), nanos: 0 },
  daily: [
    { date: '2024-01-01', count: BigInt(2) },
    { date: '2024-01-02', count: BigInt(3) },
  ],
  ...overrides,
});

describe('<AnalyticsChart>', () => {
  it('renders "Provide a code." when code is empty and does not call stats', () => {
    const stats = vi.fn();
    render(
      <AnalyticsChart
        code=""
        // biome-ignore lint/suspicious/noExplicitAny: test fake
        clients={{ analytics: { stats } as any }}
      />,
    );
    expect(screen.getByText(/provide a code\./i)).toBeInTheDocument();
    expect(stats).not.toHaveBeenCalled();
  });

  it('shows loading then total and table rows on success', async () => {
    const stats = vi.fn().mockResolvedValue(okResponse());
    renderWith({ stats });

    expect(screen.getByRole('status')).toHaveTextContent(/loading/i);

    expect(await screen.findByRole('heading', { level: 3 })).toHaveTextContent('5 clicks');
    expect(screen.getAllByTestId('daily-row')).toHaveLength(2);
    expect(stats).toHaveBeenCalledWith({ code: 'abc123', fromDate: '', toDate: '' });
  });

  it('shows error message when stats() rejects', async () => {
    const stats = vi.fn().mockRejectedValue(new Error('boom'));
    renderWith({ stats });

    expect(await screen.findByRole('alert')).toHaveTextContent(/boom/i);
  });

  it('shows "No clicks yet." when total is 0', async () => {
    const stats = vi.fn().mockResolvedValue({ code: 'abc123', total: BigInt(0), daily: [] });
    renderWith({ stats });

    expect(await screen.findByText(/no clicks yet\./i)).toBeInTheDocument();
  });

  it('re-fetches when code changes and replaces the old result', async () => {
    const stats = vi
      .fn()
      .mockResolvedValueOnce(okResponse({ code: 'abc123', total: BigInt(5) }))
      .mockResolvedValueOnce(
        okResponse({
          code: 'xyz999',
          total: BigInt(11),
          daily: [{ date: '2024-02-01', count: BigInt(11) }],
        }),
      );

    const { rerender } = renderWith({ stats });

    expect(await screen.findByRole('heading', { level: 3 })).toHaveTextContent('5 clicks');

    rerender(
      <AnalyticsChart
        code="xyz999"
        // biome-ignore lint/suspicious/noExplicitAny: test fake
        clients={{ analytics: { stats } as any }}
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 3 })).toHaveTextContent('11 clicks');
    });
    expect(screen.getAllByTestId('daily-row')).toHaveLength(1);
    expect(stats).toHaveBeenCalledTimes(2);
    expect(stats).toHaveBeenLastCalledWith({ code: 'xyz999', fromDate: '', toDate: '' });
  });

  it('renders one SVG <rect> per daily entry', async () => {
    const stats = vi.fn().mockResolvedValue(okResponse());
    const { container } = renderWith({ stats });

    await screen.findByRole('heading', { level: 3 });
    const rects = container.querySelectorAll('svg rect');
    expect(rects).toHaveLength(2);
  });

  it('forwards fromDate and toDate to the stats call', async () => {
    const stats = vi.fn().mockResolvedValue(okResponse());
    renderWith({ stats }, { fromDate: '2024-01-01', toDate: '2024-01-31' });

    await screen.findByRole('heading', { level: 3 });
    expect(stats).toHaveBeenCalledWith({
      code: 'abc123',
      fromDate: '2024-01-01',
      toDate: '2024-01-31',
    });
  });
});
