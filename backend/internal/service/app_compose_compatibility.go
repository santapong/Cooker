package service

import (
	"encoding/json"
	"sort"

	ct "github.com/compose-spec/compose-go/v2/types"
	"github.com/santapong/cooker/internal/model"
)

// Check every nonzero service field: cloud adapters must never silently drop
// settings that the preview appears to accept. Native Compose carries the
// remaining fields through its normalized project.
func composeResourceCompatibility(s ct.ServiceConfig, p *ct.Project, kind model.DeployTargetKind) []PlanDiagnostic {
	var out []PlanDiagnostic
	add := func(field, message string) {
		out = append(out, PlanDiagnostic{Service: s.Name, Field: field, Message: message})
	}
	if s.Scale != nil && *s.Scale != 1 {
		add("scale", "App deployment currently runs one replica per Compose service")
	}
	if s.Deploy != nil {
		d := s.Deploy
		if d.Replicas != nil && *d.Replicas != 1 {
			add("deploy.replicas", "App deployment currently runs one replica per Compose service")
		}
		if d.Mode != "" && d.Mode != "replicated" || len(d.Placement.Constraints) > 0 || len(d.Placement.Preferences) > 0 || d.Placement.MaxReplicas > 0 || d.UpdateConfig != nil || d.RollbackConfig != nil || d.RestartPolicy != nil || d.EndpointMode != "" || len(d.Labels) > 0 || d.Resources.Reservations != nil {
			add("deploy", "Only one replica and CPU/memory limits are supported; placement, reservations and rollout policies need a native target configuration")
		}
		if d.Resources.Limits != nil && d.Resources.Limits.Pids != 0 {
			add("deploy.resources.limits.pids", "PID limits are not supported by this deployment path")
		}
	}
	if s.Build != nil {
		data, _ := json.Marshal(s.Build)
		var fields map[string]any
		_ = json.Unmarshal(data, &fields)
		for _, k := range []string{"context", "dockerfile", "args", "target", "extensions"} {
			delete(fields, k)
		}
		if len(fields) > 0 {
			add("build", "This build uses options beyond context, Dockerfile, target and arguments; use a native build pipeline for those options")
		}
	}
	if kind == model.DeployTargetDockerHost {
		if s.Provider != nil || len(s.Models) > 0 || s.Develop != nil {
			add("provider/models/develop", "Provider plugins, model services and development actions require a separate native Compose workflow")
		}
		for _, v := range s.Volumes {
			if v.Type != "volume" || v.Source == "" {
				continue
			}
			volume := p.Volumes[v.Source]
			if bool(volume.External) || volume.Name != p.Name+"_"+v.Source || len(volume.DriverOpts) > 0 || volume.Driver != "" && volume.Driver != "local" {
				add("volumes", "Use project-scoped local named volumes without driver options; externally named volumes require a separate deployment workflow")
			}
		}
		for name := range s.Networks {
			n := p.Networks[name]
			if bool(n.External) || n.Name != p.Name+"_"+name || n.Driver != "" && n.Driver != "bridge" {
				add("networks", "Use project-scoped bridge networks so the prefix isolates this stack")
			}
		}
		if s.Build != nil && s.Platform != "" {
			add("platform", "Build platform overrides require a native build pipeline")
		}
		return out
	}
	data, _ := json.Marshal(s)
	var fields map[string]any
	_ = json.Unmarshal(data, &fields)
	allowed := []string{"profiles", "build", "image", "environment", "env_file", "depends_on", "ports", "expose", "mem_limit", "cpus", "deploy", "healthcheck", "command", "entrypoint", "networks", "labels", "pull_policy", "container_name", "volumes", "secrets", "configs", "user", "working_dir", "scale"}
	for _, k := range allowed {
		delete(fields, k)
	}
	keys := []string{}
	for k, v := range fields {
		if v != nil {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		add(k, "This Compose setting is not translated by the selected target")
	}
	if s.Command != nil && len(s.Command) == 0 {
		add("command", "Clearing an image command is not supported by this target")
	}
	if s.Entrypoint != nil && len(s.Entrypoint) == 0 {
		add("entrypoint", "Clearing an image entrypoint is not supported by this target")
	}
	for name, n := range s.Networks {
		if name != "default" || n != nil && (len(n.Aliases) > 0 || n.Ipv4Address != "" || n.Ipv6Address != "") {
			add("networks", "Compose network discovery is not available on this target; use reachable endpoint configuration")
		}
	}
	for key := range s.Labels {
		if key != model.ComposeGroupLabel {
			add("labels", "Container labels are not translated by this target")
			break
		}
	}
	if kind == model.DeployTargetKubernetes && len(s.Ports) > 1 {
		add("ports", "This Kubernetes adapter currently exposes one port per service")
	}
	return out
}
