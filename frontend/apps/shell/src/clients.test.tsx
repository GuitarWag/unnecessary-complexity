import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ClientsProvider, useClients } from './clients.js';

function Probe() {
  const c = useClients();
  return <span data-testid="probe">{c.shortener ? 'has-shortener' : 'no-shortener'}</span>;
}

describe('ClientsProvider', () => {
  it('exposes the shortener client', () => {
    render(
      <ClientsProvider baseUrl="http://localhost:8080">
        <Probe />
      </ClientsProvider>,
    );
    expect(screen.getByTestId('probe')).toHaveTextContent('has-shortener');
  });

  it('throws if useClients is called outside provider', () => {
    // suppress React's error boundary noise during the expected throw
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<Probe />)).toThrowError(/ClientsProvider/);
    spy.mockRestore();
  });
});
