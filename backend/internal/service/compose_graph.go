// Package service: compose_graph.go — extracts docker-compose YAML into a
// model.ComposeGraph. Pulled out of internal/handler/docker.go to close
// handler-layering audit Finding F3 (docs/audits/2026-05-handler-layering.md).
// The handler keeps disk + path-allowlist concerns; this file owns the
// unmarshal + graph-build loop.
package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/santapong/cooker/internal/model"
)

// ErrInvalidComposeYAML is returned by ParseComposeGraph when the input
// bytes fail to unmarshal as a docker-compose document. Callers in the
// handler layer translate this to a generic 400 to avoid leaking parser
// detail to clients.
var ErrInvalidComposeYAML = errors.New("service: invalid compose YAML")

// composeFile is the internal representation of a docker-compose YAML file.
// Private to the service package — wire shape lives in model.ComposeGraph.
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
	Labels      interface{} `yaml:"labels"`
	// Top-level (Compose v2 short form) resource limits.
	MemLimit string      `yaml:"mem_limit"`
	Cpus     interface{} `yaml:"cpus"`
	// Compose Deploy spec (`deploy.resources.limits`), which takes
	// precedence over the top-level short form when present.
	Deploy composeDeployDef `yaml:"deploy"`
}

type composeDeployDef struct {
	Resources struct {
		Limits struct {
			Memory string      `yaml:"memory"`
			CPUs   interface{} `yaml:"cpus"`
		} `yaml:"limits"`
	} `yaml:"resources"`
}

// ParseComposeGraph parses raw docker-compose YAML bytes into a
// model.ComposeGraph. Disk and path-allowlist concerns stay with the
// caller (the handler); this function takes bytes in, returns the
// fully-built domain graph out.
//
// Edges are inferred from `depends_on` and from environment-variable
// values that mention another service name. Edges are deduplicated by
// a key of the form `src->dst:type` (byte-exact — the frontend may
// already depend on this representation order).
//
// An empty document parses cleanly to a zero-value graph; invalid YAML
// returns a wrapped ErrInvalidComposeYAML.
func ParseComposeGraph(data []byte) (*model.ComposeGraph, error) {
	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidComposeYAML, err)
	}

	serviceNames := make([]string, 0, len(cf.Services))
	for name := range cf.Services {
		serviceNames = append(serviceNames, name)
	}

	graph := &model.ComposeGraph{}

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
			Labels:      parseLabels(svc.Labels),
			Resources:   parseResources(svc),
		}
		service.Group = deriveGroup(service.Labels, service.Networks)

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

	return graph, nil
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

// parseLabels normalizes the compose `labels` field, which (like
// `environment`) may be a map (`{k: v}`) or a list (`["k=v"]`). The
// dual-shape handling is identical to parseEnvToMap, so we delegate.
func parseLabels(labels interface{}) map[string]string {
	m := parseEnvToMap(labels)
	if len(m) == 0 {
		return nil
	}
	return m
}

// deriveGroup picks the deployment group-box for a service:
//  1. the com.cooker.group label, if set;
//  2. else the sole network name (Compose-idiomatic grouping), only
//     when the service is on exactly one network;
//  3. else "default".
func deriveGroup(labels map[string]string, networks []string) string {
	if g := labels[model.ComposeGroupLabel]; g != "" {
		return g
	}
	if len(networks) == 1 && networks[0] != "" {
		return networks[0]
	}
	return "default"
}

// parseResources extracts per-service CPU/memory limits. The Compose
// Deploy spec (`deploy.resources.limits`) wins over the v2 short form
// (`mem_limit`/`cpus`) when both are present. Returns nil when neither
// memory nor CPU is set.
func parseResources(svc composeServiceDef) *model.ResourceLimits {
	mem := svc.Deploy.Resources.Limits.Memory
	if mem == "" {
		mem = svc.MemLimit
	}
	cpus := stringifyCPUs(svc.Deploy.Resources.Limits.CPUs)
	if cpus == "" {
		cpus = stringifyCPUs(svc.Cpus)
	}
	if mem == "" && cpus == "" {
		return nil
	}
	return &model.ResourceLimits{
		Memory:      mem,
		MemoryBytes: parseMemoryBytes(mem),
		CPUs:        cpus,
		NanoCPUs:    parseNanoCPUs(cpus),
	}
}

// stringifyCPUs accepts the YAML-decoded cpus value (string, int, or
// float) and returns its canonical string form ("" when absent).
func stringifyCPUs(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return ""
	}
}

// parseMemoryBytes converts a compose memory string ("512m", "1g",
// "1024k", "2gb", or a bare byte count) to bytes. Returns 0 on an
// empty or unparsable value (the raw string is still preserved on the
// model for the runtime to use verbatim).
func parseMemoryBytes(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "gb"), strings.HasSuffix(s, "g"):
		mult = 1 << 30
		s = strings.TrimSuffix(strings.TrimSuffix(s, "b"), "g")
	case strings.HasSuffix(s, "mb"), strings.HasSuffix(s, "m"):
		mult = 1 << 20
		s = strings.TrimSuffix(strings.TrimSuffix(s, "b"), "m")
	case strings.HasSuffix(s, "kb"), strings.HasSuffix(s, "k"):
		mult = 1 << 10
		s = strings.TrimSuffix(strings.TrimSuffix(s, "b"), "k")
	case strings.HasSuffix(s, "b"):
		s = strings.TrimSuffix(s, "b")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || n < 0 {
		return 0
	}
	return int64(n * float64(mult))
}

// parseNanoCPUs converts a CPU count string ("0.5", "1.5") to nanoCPUs
// (1 CPU = 1e9). Returns 0 when empty or unparsable.
func parseNanoCPUs(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 {
		return 0
	}
	return int64(n * 1e9)
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
