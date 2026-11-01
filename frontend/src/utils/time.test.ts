import { describe, expect, it } from 'vitest';
import { shortId, timeAgo } from './time';

const NOW = Date.parse('2026-09-05T12:00:00Z');
const at = (ms: number) => new Date(NOW + ms).toISOString();

describe('timeAgo', () => {
  it('handles unknown input', () => {
    expect(timeAgo(undefined, NOW)).toBe('—');
    expect(timeAgo('nope', NOW)).toBe('—');
  });
  it('rounds to the nearest unit in both directions', () => {
    expect(timeAgo(at(-10_000), NOW)).toBe('just now');
    expect(timeAgo(at(10_000), NOW)).toBe('in a moment');
    expect(timeAgo(at(-4 * 60_000), NOW)).toBe('4m ago');
    expect(timeAgo(at(2 * 3_600_000), NOW)).toBe('in 2h');
    expect(timeAgo(at(-3 * 86_400_000), NOW)).toBe('3d ago');
  });
  it('falls back to a date beyond a week', () => {
    expect(timeAgo(at(-10 * 86_400_000), NOW)).toBe(new Date(NOW - 10 * 86_400_000).toLocaleDateString());
  });
});

describe('shortId', () => {
  it('takes the first eight characters', () => {
    expect(shortId('ee992d5f-5645-4f93')).toBe('ee992d5f');
    expect(shortId(undefined)).toBe('');
  });
});
