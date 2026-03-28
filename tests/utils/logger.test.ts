import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { logger, setLogLevel, getLogLevel } from '../../src/utils/logger.js';

describe('logger', () => {
  beforeEach(() => {
    setLogLevel('debug');
    vi.spyOn(console, 'log').mockImplementation(() => {});
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
    setLogLevel('info');
  });

  it('should log info messages to console.log', () => {
    logger.info('test message');
    expect(console.log).toHaveBeenCalledTimes(1);
  });

  it('should log warn messages to console.warn', () => {
    logger.warn('warning message');
    expect(console.warn).toHaveBeenCalledTimes(1);
  });

  it('should log error messages to console.error', () => {
    logger.error('error message');
    expect(console.error).toHaveBeenCalledTimes(1);
  });

  it('should log debug messages when level is debug', () => {
    setLogLevel('debug');
    logger.debug('debug message');
    expect(console.log).toHaveBeenCalledTimes(1);
  });

  it('should suppress debug messages when level is info', () => {
    setLogLevel('info');
    logger.debug('debug message');
    expect(console.log).not.toHaveBeenCalled();
  });

  it('should suppress info messages when level is warn', () => {
    setLogLevel('warn');
    logger.info('info message');
    expect(console.log).not.toHaveBeenCalled();
  });

  it('should get and set log level', () => {
    setLogLevel('error');
    expect(getLogLevel()).toBe('error');
  });

  it('should include data in log output', () => {
    logger.info('test', { key: 'value' });
    expect(console.log).toHaveBeenCalledTimes(1);
    const output = vi.mocked(console.log).mock.calls[0][0] as string;
    expect(output).toContain('value');
  });
});
