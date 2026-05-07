import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Preset, Status } from '@url-shortener/proto-web/loadgen';
import { describe, expect, it, vi } from 'vitest';
import { LoadTest } from './LoadTest.js';

interface FakeLoadGen {
  runLoadTest: ReturnType<typeof vi.fn>;
}

const renderWith = (loadgen: FakeLoadGen) =>
  render(
    <LoadTest
      // biome-ignore lint/suspicious/noExplicitAny: test fake; mirrors createClient shape
      clients={{ loadgen: loadgen as any }}
    />,
  );

function asyncIterable<T>(items: T[], opts: { delayMs?: number } = {}): AsyncIterable<T> {
  return {
    [Symbol.asyncIterator]() {
      let i = 0;
      return {
        async next() {
          if (i >= items.length) return { value: undefined as never, done: true };
          if (opts.delayMs) {
            await new Promise((resolve) => setTimeout(resolve, opts.delayMs));
          }
          const value = items[i++];
          return { value, done: false };
        },
      };
    },
  };
}

describe('<LoadTest>', () => {
  it('renders all six preset cards', () => {
    renderWith({ runLoadTest: vi.fn() });
    expect(screen.getByTestId('preset-low')).toBeInTheDocument();
    expect(screen.getByTestId('preset-medium')).toBeInTheDocument();
    expect(screen.getByTestId('preset-high')).toBeInTheDocument();
    expect(screen.getByTestId('preset-xhigh')).toBeInTheDocument();
    expect(screen.getByTestId('preset-xxhigh')).toBeInTheDocument();
    expect(screen.getByTestId('preset-insane')).toBeInTheDocument();
  });

  it('selecting a preset card flips aria-checked on click', async () => {
    renderWith({ runLoadTest: vi.fn() });
    const user = userEvent.setup();

    const high = screen.getByTestId('preset-high');
    expect(high).toHaveAttribute('aria-checked', 'false');
    await user.click(high);
    expect(high).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByTestId('preset-low')).toHaveAttribute('aria-checked', 'false');
  });

  it('runs the selected preset and streams RUNNING samples into the live numbers', async () => {
    const samples = [
      {
        elapsedSeconds: 1,
        currentRps: 90,
        totalRequests: BigInt(90),
        errors: BigInt(0),
        p50Ms: 5,
        p95Ms: 12,
        p99Ms: 18,
        status: Status.RUNNING,
        message: '',
      },
      {
        elapsedSeconds: 2,
        currentRps: 100,
        totalRequests: BigInt(190),
        errors: BigInt(0),
        p50Ms: 6,
        p95Ms: 14,
        p99Ms: 22,
        status: Status.RUNNING,
        message: '',
      },
    ];
    const runLoadTest = vi.fn().mockImplementation(() => asyncIterable(samples));
    renderWith({ runLoadTest });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /^run$/i }));

    await waitFor(() => {
      expect(screen.getByTestId('spark-current-RPS')).toHaveTextContent(/100/);
    });
    expect(screen.getByTestId('spark-p99-latency')).toHaveTextContent(/22/);
    expect(runLoadTest).toHaveBeenCalledTimes(1);
    const arg = runLoadTest.mock.calls[0][0];
    expect(arg.preset).toBe(Preset.LOW);
  });

  it('flips status pill and shows summary when terminal COMPLETED arrives', async () => {
    const samples = [
      {
        elapsedSeconds: 1,
        currentRps: 50,
        totalRequests: BigInt(50),
        errors: BigInt(0),
        p50Ms: 4,
        p95Ms: 10,
        p99Ms: 15,
        status: Status.RUNNING,
        message: '',
      },
      {
        elapsedSeconds: 2,
        currentRps: 0,
        totalRequests: BigInt(120),
        errors: BigInt(2),
        p50Ms: 5,
        p95Ms: 11,
        p99Ms: 16,
        status: Status.COMPLETED,
        message: '',
      },
    ];
    const runLoadTest = vi.fn().mockImplementation(() => asyncIterable(samples));
    renderWith({ runLoadTest });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /^run$/i }));

    await waitFor(() => {
      expect(screen.getByTestId('status-pill')).toHaveTextContent(/completed/i);
    });
    const summary = screen.getByTestId('summary');
    expect(summary).toHaveTextContent(/total requests/i);
    expect(summary).toHaveTextContent(/120/);
    expect(summary).toHaveTextContent(/errors/i);
    expect(summary).toHaveTextContent(/^.*?2.*$/);
  });

  it('surfaces a FAILED terminal sample in the alert', async () => {
    const runLoadTest = vi.fn().mockImplementation(() =>
      asyncIterable([
        {
          elapsedSeconds: 1,
          currentRps: 0,
          totalRequests: BigInt(0),
          errors: BigInt(0),
          p50Ms: 0,
          p95Ms: 0,
          p99Ms: 0,
          status: Status.FAILED,
          message: 'seed failed',
        },
      ]),
    );
    renderWith({ runLoadTest });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /^run$/i }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/seed failed/i);
    });
    expect(screen.getByTestId('status-pill')).toHaveTextContent(/failed/i);
  });
});
