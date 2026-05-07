import { describe, expect, it } from 'vitest';
import { createClients } from './index.js';

describe('createClients', () => {
  it('returns four service clients backed by the same transport', () => {
    const c = createClients({ baseUrl: 'http://localhost:8080' });
    expect(c.shortener).toBeDefined();
    expect(c.resolver).toBeDefined();
    expect(c.analytics).toBeDefined();
    expect(c.loadgen).toBeDefined();
    expect(c.transport).toBeDefined();
  });
});
