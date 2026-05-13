package handler

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
)

// composeBaseDir is the only directory ParseComposeFile reads from.
// Overridable at runtime (tests and future configurability) via
// SetComposeBaseDir. An authenticated caller can name a file inside
// this directory but can never escape it.
var composeBaseDir = "."

// SetComposeBaseDir sets the base directory the compose-parse
// handler reads from. Intended for tests and for the server to
// point at a dedicated config dir at boot.
func SetComposeBaseDir(dir string) { composeBaseDir = dir }

// resolveComposePath validates name and returns an absolute path
// inside composeBaseDir. Rejects anything that looks like a path
// (contains "/" or "\", starts with ".", is absolute, or resolves
// outside the base after cleaning). Callers get a generic error so
// the response never reveals the base directory.
func resolveComposePath(name string) (string, error) {
	if name == "" {
		name = "docker-compose.yml"
	}
	if strings.ContainsAny(name, `/\`) {
		return "", errors.New("invalid filename")
	}
	if strings.HasPrefix(name, ".") {
		// Blocks "..", ".env", and hidden-file probing.
		return "", errors.New("invalid filename")
	}
	if filepath.IsAbs(name) {
		return "", errors.New("invalid filename")
	}
	baseAbs, err := filepath.Abs(composeBaseDir)
	if err != nil {
		return "", errors.New("server misconfigured")
	}
	candidate := filepath.Join(baseAbs, name)
	// Defence in depth: even though the checks above reject separators
	// we re-verify containment after path resolution.
	if !strings.HasPrefix(candidate, baseAbs+string(os.PathSeparator)) && candidate != baseAbs {
		return "", errors.New("invalid filename")
	}
	return candidate, nil
}

func ListDockerImages(c *gin.Context) {
	// Placeholder: will use Docker Engine SDK
	images := []model.ImageInfo{}
	c.JSON(http.StatusOK, images)
}

func GetDockerImage(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"id":          c.Param("id"),
		"message":     "Image details with OCI manifest will be populated via Docker Engine SDK",
		"ociManifest": nil,
	})
}

func BuildDockerImage(c *gin.Context) {
	var req struct {
		Dockerfile string            `json:"dockerfile"`
		Context    string            `json:"context"`
		Tags       []string          `json:"tags"`
		BuildArgs  map[string]string `json:"buildArgs"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Placeholder: will stream build output via WebSocket
	c.JSON(http.StatusAccepted, gin.H{
		"buildId": "build-placeholder",
		"message": "Build initiated. Connect to WS /ws/docker/build/<buildId> for logs.",
	})
}

func DeleteDockerImage(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "image deleted", "id": c.Param("id")})
}

func ListContainers(c *gin.Context) {
	containers := []model.ContainerInfo{}
	c.JSON(http.StatusOK, containers)
}

func CreateContainer(c *gin.Context) {
	var req struct {
		Image   string              `json:"image" binding:"required"`
		Name    string              `json:"name"`
		Ports   []model.PortBinding `json:"ports"`
		Env     map[string]string   `json:"env"`
		Command []string            `json:"command"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":      "container-placeholder",
		"message": "Container created via Docker Engine SDK",
	})
}

func StopContainer(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "stopped", "id": c.Param("id")})
}

func DeleteContainer(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "removed", "id": c.Param("id")})
}

func GetContainerLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"logs": "", "message": "Stream via WebSocket for live logs"})
}

// ParseComposeFile resolves the requested compose filename inside the
// allowlisted base directory, reads the bytes, and hands them to
// service.ParseComposeGraph. All graph-construction logic lives in the
// service layer (see compose_graph.go); the handler owns disk + path
// allowlist + HTTP framing only.
func ParseComposeFile(c *gin.Context) {
	var req struct {
		ComposePath string `json:"composePath"`
	}
	_ = c.ShouldBindJSON(&req)

	resolved, err := resolveComposePath(req.ComposePath)
	if err != nil {
		// Intentionally generic: the concrete reason (absolute path,
		// traversal, separator) doesn't help a legitimate caller and
		// helps an attacker map the allowlist.
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid compose filename"})
		return
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		// Don't echo the resolved path — it would leak composeBaseDir.
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read compose file"})
		return
	}

	graph, err := service.ParseComposeGraph(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid YAML"})
		return
	}

	c.JSON(http.StatusOK, graph)
}

func UpdateComposeService(c *gin.Context) {
	name := c.Param("name")
	var req struct {
		Environment map[string]string `json:"environment"`
		Ports       []string          `json:"ports"`
		Image       string            `json:"image"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Service config updated",
		"service": name,
	})
}
