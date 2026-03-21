// OCI Media Types - Reference: https://github.com/opencontainers/image-spec/blob/main/media-types.md
export const OCI_MEDIA_TYPES = {
  IMAGE_MANIFEST: 'application/vnd.oci.image.manifest.v1+json',
  IMAGE_INDEX: 'application/vnd.oci.image.index.v1+json',
  IMAGE_CONFIG: 'application/vnd.oci.image.config.v1+json',
  IMAGE_LAYER_GZIP: 'application/vnd.oci.image.layer.v1.tar+gzip',
  IMAGE_LAYER_ZSTD: 'application/vnd.oci.image.layer.v1.tar+zstd',
  EMPTY_JSON: 'application/vnd.oci.empty.v1+json',
} as const;

export function isOCIManifest(mediaType: string): boolean {
  return mediaType === OCI_MEDIA_TYPES.IMAGE_MANIFEST
    || mediaType === 'application/vnd.docker.distribution.manifest.v2+json';
}

export function isOCIIndex(mediaType: string): boolean {
  return mediaType === OCI_MEDIA_TYPES.IMAGE_INDEX
    || mediaType === 'application/vnd.docker.distribution.manifest.list.v2+json';
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
