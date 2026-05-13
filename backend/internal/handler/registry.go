package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/oci"
)

func ListRepositories(c *gin.Context) {
	// Placeholder: will use go-containerregistry to list repos
	// Implements OCI distribution-spec: GET /v2/_catalog
	c.JSON(http.StatusOK, gin.H{
		"repositories": []string{},
		"ociSpec":      "distribution-spec v1.1",
	})
}

func ListTags(c *gin.Context) {
	name := c.Param("name")
	// Implements OCI distribution-spec: GET /v2/<name>/tags/list
	c.JSON(http.StatusOK, gin.H{
		"name": name,
		"tags": []string{},
	})
}

func GetManifest(c *gin.Context) {
	name := c.Param("name")
	ref := c.Param("ref")
	// Implements OCI distribution-spec: GET /v2/<name>/manifests/<reference>
	// Returns OCI Image Manifest or Image Index
	c.JSON(http.StatusOK, gin.H{
		"name":      name,
		"reference": ref,
		"manifest":  nil,
		"mediaType": oci.MediaTypeImageManifest,
	})
}

func PushImage(c *gin.Context) {
	var req struct {
		Image    string `json:"image" binding:"required"`
		Registry string `json:"registry" binding:"required"`
		Tag      string `json:"tag"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Implements OCI distribution-spec push sequence:
	// 1. POST /v2/<name>/blobs/uploads/ (initiate blob upload)
	// 2. PUT /v2/<name>/blobs/uploads/<uuid> (upload blob)
	// 3. PUT /v2/<name>/manifests/<reference> (push manifest)
	c.JSON(http.StatusAccepted, gin.H{
		"message":  "push initiated",
		"image":    req.Image,
		"registry": req.Registry,
	})
}

func PullImage(c *gin.Context) {
	var req struct {
		Image    string `json:"image" binding:"required"`
		Registry string `json:"registry"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "pull initiated",
		"image":   req.Image,
	})
}

func GetReferrers(c *gin.Context) {
	name := c.Param("name")
	digest := c.Param("digest")
	// Implements OCI distribution-spec v1.1 referrers API:
	// GET /v2/<name>/referrers/<digest>
	// Returns artifacts (signatures, SBOMs, build provenance) referencing this image
	c.JSON(http.StatusOK, gin.H{
		"name":      name,
		"digest":    digest,
		"referrers": []interface{}{},
		"ociSpec":   "distribution-spec v1.1 referrers API",
	})
}

// Settings handlers for registry configuration
func ListRegistryConfigs(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

func AddRegistryConfig(c *gin.Context) {
	var req struct {
		Name     string `json:"name" binding:"required"`
		URL      string `json:"url" binding:"required"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "registry added", "name": req.Name})
}

func DeleteRegistryConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "registry removed", "id": c.Param("id")})
}

func ListClusterConfigs(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

func AddClusterConfig(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		KubeConfig string `json:"kubeconfig"`
		Context    string `json:"context"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "cluster added", "name": req.Name})
}
