package oci

// OCI media types as defined in the OCI image-spec v1.1.
// Reference: https://github.com/opencontainers/image-spec/blob/main/media-types.md
const (
	// Image manifest media types
	MediaTypeImageManifest = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeImageIndex    = "application/vnd.oci.image.index.v1+json"

	// Image config media type
	MediaTypeImageConfig = "application/vnd.oci.image.config.v1+json"

	// Layer media types
	MediaTypeImageLayer            = "application/vnd.oci.image.layer.v1.tar"
	MediaTypeImageLayerGzip        = "application/vnd.oci.image.layer.v1.tar+gzip"
	MediaTypeImageLayerZstd        = "application/vnd.oci.image.layer.v1.tar+zstd"
	MediaTypeImageLayerNonDist     = "application/vnd.oci.image.layer.nondistributable.v1.tar"
	MediaTypeImageLayerNonDistGzip = "application/vnd.oci.image.layer.nondistributable.v1.tar+gzip"

	// Docker compatibility media types
	MediaTypeDockerManifestV2 = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	MediaTypeDockerImageConfig = "application/vnd.docker.container.image.v1+json"
	MediaTypeDockerLayerGzip   = "application/vnd.docker.image.rootfs.diff.tar.gzip"

	// OCI artifact types (distribution-spec v1.1 referrers API)
	MediaTypeEmptyJSON = "application/vnd.oci.empty.v1+json"
)

// IsOCIManifest checks if a media type is an OCI manifest type.
func IsOCIManifest(mediaType string) bool {
	return mediaType == MediaTypeImageManifest || mediaType == MediaTypeDockerManifestV2
}

// IsOCIIndex checks if a media type is an OCI image index / manifest list.
func IsOCIIndex(mediaType string) bool {
	return mediaType == MediaTypeImageIndex || mediaType == MediaTypeDockerManifestList
}
