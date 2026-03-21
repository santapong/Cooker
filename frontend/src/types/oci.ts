// OCI image-spec v1.1 types
// Reference: https://github.com/opencontainers/image-spec

export interface OCIDescriptor {
  mediaType: string;
  digest: string;
  size: number;
  urls?: string[];
  annotations?: Record<string, string>;
  platform?: OCIPlatform;
}

export interface OCIManifest {
  schemaVersion: number;
  mediaType: string;
  config: OCIDescriptor;
  layers: OCIDescriptor[];
  annotations?: Record<string, string>;
}

export interface OCIIndex {
  schemaVersion: number;
  mediaType: string;
  manifests: OCIDescriptor[];
  annotations?: Record<string, string>;
}

export interface OCIPlatform {
  architecture: string;
  os: string;
  variant?: string;
}

// OCI media type constants
export const OCI_MEDIA_TYPES = {
  IMAGE_MANIFEST: 'application/vnd.oci.image.manifest.v1+json',
  IMAGE_INDEX: 'application/vnd.oci.image.index.v1+json',
  IMAGE_CONFIG: 'application/vnd.oci.image.config.v1+json',
  IMAGE_LAYER_GZIP: 'application/vnd.oci.image.layer.v1.tar+gzip',
  IMAGE_LAYER_ZSTD: 'application/vnd.oci.image.layer.v1.tar+zstd',
  EMPTY_JSON: 'application/vnd.oci.empty.v1+json',
} as const;
