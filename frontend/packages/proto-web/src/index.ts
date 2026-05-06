import { createClient } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { AnalyticsService } from './gen/analytics/v1/analytics_pb.js';
import { ResolverService } from './gen/resolver/v1/resolver_pb.js';
import { ShortenerService } from './gen/shortener/v1/shortener_pb.js';

export type Transport = ReturnType<typeof createConnectTransport>;

export interface ClientsOptions {
  /** Gateway base URL, e.g. https://api.example.com */
  baseUrl: string;
  /** Override fetch (mostly for tests). */
  fetch?: typeof globalThis.fetch;
}

export interface Clients {
  shortener: ReturnType<typeof createClient<typeof ShortenerService>>;
  resolver: ReturnType<typeof createClient<typeof ResolverService>>;
  analytics: ReturnType<typeof createClient<typeof AnalyticsService>>;
  transport: Transport;
}

export function createClients(opts: ClientsOptions): Clients {
  const transport = createConnectTransport({
    baseUrl: opts.baseUrl,
    fetch: opts.fetch ?? globalThis.fetch,
  });
  return {
    shortener: createClient(ShortenerService, transport),
    resolver: createClient(ResolverService, transport),
    analytics: createClient(AnalyticsService, transport),
    transport,
  };
}

export { ShortenerService, ResolverService, AnalyticsService };
