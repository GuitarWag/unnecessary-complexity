import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { CreateButton } from './CreateButton.js';

describe('<CreateButton>', () => {
  it('renders default "Create" label when no children given', () => {
    render(<CreateButton />);
    expect(screen.getByRole('button')).toHaveTextContent('Create');
  });

  it('calls onClick once when clicked', async () => {
    const onClick = vi.fn();
    render(<CreateButton onClick={onClick}>Go</CreateButton>);
    const user = userEvent.setup();

    await user.click(screen.getByRole('button', { name: /go/i }));

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('disables itself while a Promise-returning onClick is in flight, then re-enables', async () => {
    let resolve!: () => void;
    const pending = new Promise<void>((r) => {
      resolve = r;
    });
    const onClick = vi.fn().mockReturnValue(pending);

    render(<CreateButton onClick={onClick}>Go</CreateButton>);
    const user = userEvent.setup();
    const button = screen.getByRole('button', { name: /go/i });

    await user.click(button);

    expect(button).toBeDisabled();

    await act(async () => {
      resolve();
      await pending;
    });

    expect(button).not.toBeDisabled();
  });

  it('shows the busy label while pending', async () => {
    let resolve!: () => void;
    const pending = new Promise<void>((r) => {
      resolve = r;
    });
    const onClick = vi.fn().mockReturnValue(pending);

    render(
      <CreateButton onClick={onClick} busyLabel="Working…">
        Go
      </CreateButton>,
    );
    const user = userEvent.setup();

    await user.click(screen.getByRole('button', { name: /go/i }));

    expect(screen.getByRole('button')).toHaveTextContent('Working…');

    await act(async () => {
      resolve();
      await pending;
    });
  });

  it('honors the disabled prop', () => {
    render(<CreateButton disabled>Go</CreateButton>);
    expect(screen.getByRole('button', { name: /go/i })).toBeDisabled();
  });

  it('applies cb--secondary className when variant="secondary"', () => {
    render(<CreateButton variant="secondary">Go</CreateButton>);
    const button = screen.getByRole('button', { name: /go/i });
    expect(button).toHaveClass('cb');
    expect(button).toHaveClass('cb--secondary');
  });

  it('logs and recovers when onClick rejects', async () => {
    const error = new Error('boom');
    const onClick = vi.fn().mockRejectedValue(error);
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    render(<CreateButton onClick={onClick}>Go</CreateButton>);
    const user = userEvent.setup();
    const button = screen.getByRole('button', { name: /go/i });

    await user.click(button);

    expect(onClick).toHaveBeenCalledTimes(1);
    expect(errSpy).toHaveBeenCalled();
    expect(button).not.toBeDisabled();

    errSpy.mockRestore();
  });
});
