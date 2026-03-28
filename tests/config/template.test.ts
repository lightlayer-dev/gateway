import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { parse } from 'yaml';

describe('gateway.yaml template', () => {
  const templatePath = resolve(import.meta.dirname, '../../templates/gateway.yaml');
  const raw = readFileSync(templatePath, 'utf-8');

  it('should be valid YAML', () => {
    const parsed = parse(raw);
    expect(parsed).toBeDefined();
    expect(typeof parsed).toBe('object');
  });

  it('should have gateway.listen config', () => {
    const parsed = parse(raw);
    expect(parsed.gateway.listen.port).toBe(8080);
    expect(parsed.gateway.listen.host).toBe('0.0.0.0');
  });

  it('should have gateway.origin config', () => {
    const parsed = parse(raw);
    expect(parsed.gateway.origin.url).toBe('https://api.example.com');
  });

  it('should have plugins section', () => {
    const parsed = parse(raw);
    expect(parsed.plugins).toBeDefined();
    expect(parsed.plugins.discovery.enabled).toBe(true);
    expect(parsed.plugins.identity.enabled).toBe(true);
    expect(parsed.plugins.payments.enabled).toBe(false);
    expect(parsed.plugins.rate_limits.enabled).toBe(true);
    expect(parsed.plugins.analytics.enabled).toBe(true);
    expect(parsed.plugins.security.enabled).toBe(true);
  });

  it('should have admin section', () => {
    const parsed = parse(raw);
    expect(parsed.admin.enabled).toBe(true);
    expect(parsed.admin.port).toBe(9090);
  });
});
