import { describe, it, expect } from 'vitest';
import { formatBytes, formatSpace } from './formatBytes';

describe('formatBytes', () => {
  it('formats raw bytes', () => expect(formatBytes(500)).toBe('500 o'));
  it('formats kilobytes', () => expect(formatBytes(1024)).toBe('1.0 Ko'));
  it('formats megabytes', () => expect(formatBytes(1048576)).toBe('1.0 Mo'));
  it('formats gigabytes', () => expect(formatBytes(1073741824)).toBe('1.0 Go'));
  it('formats 0 bytes', () => expect(formatBytes(0)).toBe('0 o'));
});

describe('formatSpace', () => {
  it('returns N/A for zero', () => expect(formatSpace(0)).toBe('N/A'));
  it('returns N/A for negative', () => expect(formatSpace(-1)).toBe('N/A'));
  it('formats megabytes', () => expect(formatSpace(524288000)).toBe('500 Mo'));
  it('formats gigabytes', () => expect(formatSpace(1073741824)).toBe('1.0 Go'));
});
