import { describe, it, expect } from 'vitest';
import { formatRelative } from './formatRelative';

describe('formatRelative', () => {
  it("returns \"à l'instant\" for recent dates", () => {
    expect(formatRelative(new Date().toISOString())).toBe("à l'instant");
  });

  it('handles empty string', () => {
    expect(formatRelative('')).toBe('—');
  });

  it('formats minutes ago', () => {
    const fiveMinutesAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString();
    expect(formatRelative(fiveMinutesAgo)).toBe('il y a 5 min');
  });

  it('formats hours ago', () => {
    const twoHoursAgo = new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString();
    expect(formatRelative(twoHoursAgo)).toBe('il y a 2 h');
  });
});
