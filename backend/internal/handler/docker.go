package handler

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"github.com/cooker-ci/cooker/internal/model"
)

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
		Image   string            `json:"image" binding:"required"`
		Name    string            `json:"name"`
		Ports   []model.PortBinding `json:"ports"`
		Env     map[string]string `json:"env"`
		Command []string          `json:"command"`
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

// composeFile is the internal representation of a docker-compose YAML file.
type composeFile struct {
	Services map[string]composeServiceDef `yaml:"services"`
	Networks map[string]interface{}       `yaml:"networks"`
	Volumes  map[string]interface{}       `yaml:"volumes"`
}

type composeServiceDef struct {
	Image       string      `yaml:"image"`
	Build       interface{} `yaml:"build"`
	Ports       []string    `yaml:"ports"`
	Environment interface{} `yaml:"environment"`
	DependsOn   interface{} `yaml:"depends_on"`
	Networks    []string    `yaml:"networks"`
	Volumes     []string    `yaml:"volumes"`
	Command     interface{} `yaml:"command"`
}

func ParseComposeFile(c *gin.Context) {
	var req struct {
		ComposePath string `json:"composePath"`
	}
	_ = c.ShouldBindJSON(&req)

	path := req.ComposePath
	if path == "" {
		path = "docker-compose.yml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot read compose file: " + err.Error()})
		return
	}

	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid YAML: " + err.Error()})
		return
	}

	serviceNames := make([]string, 0, len(cf.Services))
	for name := range cf.Services {
		serviceNames = append(serviceNames, name)
	}

	graph := model.ComposeGraph{}

	// Collect network and volume names
	for name := range cf.Networks {
		graph.Networks = append(graph.Networks, name)
	}
	for name := range cf.Volumes {
		graph.Volumes = append(graph.Volumes, name)
	}

	connSet := make(map[string]bool)

	for name, svc := range cf.Services {
		service := model.ComposeService{
			Name:        name,
			Image:       svc.Image,
			Ports:       svc.Ports,
			Environment: parseEnvToMap(svc.Environment),
			DependsOn:   parseDependsOn(svc.DependsOn),
			Networks:    svc.Networks,
			Volumes:     svc.Volumes,
			Command:     parseCommand(svc.Command),
			Status:      "unknown",
		}

		if svc.Build != nil {
			service.Build = parseBuild(svc.Build)
		}

		graph.Services = append(graph.Services, service)

		// Connections from depends_on
		for _, dep := range service.DependsOn {
			key := name + "->" + dep + ":depends_on"
			if !connSet[key] {
				connSet[key] = true
				graph.Connections = append(graph.Connections, model.ComposeConnection{
					Source: name,
					Target: dep,
					Type:   "depends_on",
					Label:  "depends_on",
				})
			}
		}

		// Connections from environment variable references
		for envKey, envVal := range service.Environment {
			for _, other := range serviceNames {
				if other != name && strings.Contains(envVal, other) {
					key := name + "->" + other + ":env_reference"
					if !connSet[key] {
						connSet[key] = true
						graph.Connections = append(graph.Connections, model.ComposeConnection{
							Source: name,
							Target: other,
							Type:   "env_reference",
							Label:  envKey,
						})
					}
				}
			}
		}
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

func parseEnvToMap(env interface{}) map[string]string {
	result := make(map[string]string)
	if env == nil {
		return result
	}
	switch v := env.(type) {
	case map[string]interface{}:
		for key, val := range v {
			if s, ok := val.(string); ok {
				result[key] = s
			}
		}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts := strings.SplitN(s, "=", 2)
				if len(parts) == 2 {
					result[parts[0]] = parts[1]
				} else {
					result[parts[0]] = ""
				}
			}
		}
	}
	return result
}

func parseDependsOn(dep interface{}) []string {
	if dep == nil {
		return nil
	}
	switch v := dep.(type) {
	case []interface{}:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case map[string]interface{}:
		var result []string
		for name := range v {
			result = append(result, name)
		}
		return result
	}
	return nil
}

func parseCommand(cmd interface{}) string {
	if cmd == nil {
		return ""
	}
	switch v := cmd.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func parseBuild(build interface{}) *model.ComposeBuild {
	switch v := build.(type) {
	case string:
		return &model.ComposeBuild{Context: v}
	case map[string]interface{}:
		b := &model.ComposeBuild{}
		if ctx, ok := v["context"].(string); ok {
			b.Context = ctx
		}
		if df, ok := v["dockerfile"].(string); ok {
			b.Dockerfile = df
		}
		return b
	}
	return nil
}
