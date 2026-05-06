import { type Clients, createClients } from '@url-shortener/proto-web';
import { type ReactNode, createContext, useContext, useMemo } from 'react';

const ClientsContext = createContext<Clients | null>(null);

export interface ClientsProviderProps {
  baseUrl: string;
  /** Inject pre-built clients (used by tests). */
  clients?: Clients;
  children: ReactNode;
}

export function ClientsProvider({ baseUrl, clients, children }: ClientsProviderProps) {
  const value = useMemo(() => clients ?? createClients({ baseUrl }), [baseUrl, clients]);
  return <ClientsContext.Provider value={value}>{children}</ClientsContext.Provider>;
}

export function useClients(): Clients {
  const ctx = useContext(ClientsContext);
  if (!ctx) throw new Error('useClients must be used within <ClientsProvider>');
  return ctx;
}
