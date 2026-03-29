import { describe, it, expect } from 'vitest';
import { OCI_MEDIA_TYPES, isOCIManifest, isOCIIndex, formatSize } from './ociMediaTypes';

describe('isOCIManifest', () => {
  it('returns true for OCI image manifest', () => {
    expect(isOCIManifest(OCI_MEDIA_TYPES.IMAGE_MANIFEST)).toBe(true);
  });

  it('returns true for Docker manifest v2', () => {
    expect(isOCIManifest('application/vnd.docker.distribution.manifest.v2+json')).toBe(true);
  });

  it('returns false for OCI index', () => {
    expect(isOCIManifest(OCI_MEDIA_TYPES.IMAGE_INDEX)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isOCIManifest('')).toBe(false);
  });

  it('returns false for random media type', () => {
    expect(isOCIManifest('text/plain')).toBe(false);
  });
});

describe('isOCIIndex', () => {
  it('returns true for OCI image index', () => {
    expect(isOCIIndex(OCI_MEDIA_TYPES.IMAGE_INDEX)).toBe(true);
  });

  it('returns true for Docker manifest list', () => {
    expect(isOCIIndex('application/vnd.docker.distribution.manifest.list.v2+json')).toBe(true);
  });

  it('returns false for OCI manifest', () => {
    expect(isOCIIndex(OCI_MEDIA_TYPES.IMAGE_MANIFEST)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isOCIIndex('')).toBe(false);
  });
});

describe('formatSize', () => {
  it('formats bytes', () => {
    expect(formatSize(500)).toBe('500 B');
  });

  it('formats zero bytes', () => {
    expect(formatSize(0)).toBe('0 B');
  });

  it('formats kilobytes', () => {
    expect(formatSize(2048)).toBe('2.0 KB');
  });

  it('formats megabytes', () => {
    expect(formatSize(5 * 1024 * 1024)).toBe('5.0 MB');
  });

  it('formats gigabytes', () => {
    expect(formatSize(2.5 * 1024 * 1024 * 1024)).toBe('2.50 GB');
  });

  it('formats exactly 1 KB', () => {
    expect(formatSize(1024)).toBe('1.0 KB');
  });

  it('formats exactly 1 MB', () => {
    expect(formatSize(1024 * 1024)).toBe('1.0 MB');
  });

  it('formats exactly 1 GB', () => {
    expect(formatSize(1024 * 1024 * 1024)).toBe('1.00 GB');
  });
});
