package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/template"
	ct "github.com/compose-spec/compose-go/v2/types"
	"github.com/distribution/reference"
	"gopkg.in/yaml.v3"

	"github.com/santapong/cooker/internal/deploy/deploytarget/ecs"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
)

type ComposeCandidate struct {
	Path     string   `json:"path"`
	Services []string `json:"services"`
	Error    string   `json:"error,omitempty"`
}
type PlanDiagnostic struct {
	Service string `json:"service,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}
type BuildFilePreview struct {
	Service    string `json:"service"`
	Context    string `json:"context"`
	Dockerfile string `json:"dockerfile"`
	Content    string `json:"content"`
}
type RepositoryInspection struct {
	Commit            string              `json:"commit"`
	Candidates        []ComposeCandidate  `json:"candidates"`
	Graph             *model.ComposeGraph `json:"graph,omitempty"`
	BuildFiles        []BuildFilePreview  `json:"buildFiles"`
	Diagnostics       []PlanDiagnostic    `json:"diagnostics"`
	RequiredVariables []string            `json:"requiredVariables"`
	Profiles          []string            `json:"profiles"`
	YAML              string              `json:"yaml,omitempty"`
	Workloads         map[string]string   `json:"workloads"`
	Deployable        bool                `json:"deployable"`
}

func safeRelativePath(p string) bool {
	p = filepath.Clean(p)
	return p != ".." && !filepath.IsAbs(p) && !strings.HasPrefix(p, ".."+string(filepath.Separator)) && !strings.ContainsAny(p, "\x00\r\n")
}

// repositoryPath follows symlinks before enforcing the checkout boundary.
// Parent-relative Compose paths are valid if their final destination is inside it.
func repositoryPath(root, base, path string) (string, error) {
	p := path
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	p, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("source path %q is missing or unreadable", path)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, p)
	if err != nil || !safeRelativePath(rel) {
		return "", fmt.Errorf("source path %q leaves the repository", path)
	}
	if rel == ".git" || strings.HasPrefix(filepath.ToSlash(rel), ".git/") {
		return "", fmt.Errorf("git metadata cannot be used as a build input")
	}
	return p, nil
}

func readSourceFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source file is not a regular file")
	}
	if info.Size() > 1<<20 {
		return nil, fmt.Errorf("source file exceeds the 1 MiB preview limit")
	}
	return os.ReadFile(path)
}

func discoverCompose(ctx context.Context, root string) ([]ComposeCandidate, error) {
	items := []ComposeCandidate{}
	visited := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		visited++
		if visited > 20000 {
			return fmt.Errorf("repository exceeds the 20,000 entry inspection limit")
		}
		if d.IsDir() {
			if path != root && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") || !strings.Contains(name, "compose") {
			return nil
		}
		if len(items) >= 200 {
			return fmt.Errorf("repository has more than 200 Compose candidates; enter a specific path")
		}
		rel, _ := filepath.Rel(root, path)
		item := ComposeCandidate{Path: filepath.ToSlash(rel), Services: []string{}}
		data, err := readSourceFile(path)
		var doc struct {
			Services map[string]any `yaml:"services"`
		}
		if err == nil {
			err = yaml.Unmarshal(data, &doc)
		}
		if err != nil {
			item.Error = "Cannot read this Compose file"
		} else {
			for key := range doc.Services {
				item.Services = append(item.Services, key)
			}
			sort.Strings(item.Services)
		}
		items = append(items, item)
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, err
}

func (d *AppDetector) Inspect(ctx context.Context, app *model.App, env map[string]string) (*RepositoryInspection, error) {
	ctx, cancel := context.WithTimeout(ctx, appDetectTimeout)
	defer cancel()
	opts := github.CloneOptions{Repo: app.GitHubRepo, Branch: app.Branch, Depth: 1}
	if app.BuildPlan != nil {
		opts.Commit = app.BuildPlan.Commit
		opts.InstallationID = app.BuildPlan.InstallationID
	}
	root, err := d.cloneFn(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	commit, err := github.HeadCommit(ctx, root)
	if err != nil {
		return nil, err
	}
	if opts.Commit != "" && commit != opts.Commit {
		return nil, fmt.Errorf("checkout does not match the reviewed commit")
	}
	result := &RepositoryInspection{Commit: commit, Candidates: []ComposeCandidate{}, BuildFiles: []BuildFilePreview{}, Diagnostics: []PlanDiagnostic{}, RequiredVariables: []string{}, Profiles: []string{}, Workloads: map[string]string{}}
	result.Candidates, err = discoverCompose(ctx, root)
	if err != nil {
		return nil, err
	}
	if app.BuildPlan == nil || (len(app.BuildPlan.Files) == 0 && app.BuildPlan.Path == "") {
		return result, nil
	}
	if app.BuildPlan.Kind != model.BuildPlanCompose {
		return nil, fmt.Errorf("repository preview currently expects a Compose build plan")
	}
	resolved, err := loadRepositoryCompose(ctx, root, app, env)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Field: "compose", Message: err.Error()})
		return result, nil
	}
	result.Graph = resolved.Graph
	result.BuildFiles = resolved.BuildFiles
	result.Diagnostics = resolved.Diagnostics
	result.RequiredVariables = resolved.RequiredVariables
	result.Profiles = resolved.Profiles
	result.YAML = resolved.YAML
	if d.CheckExecution != nil {
		hasBuild := false
		for _, s := range result.Graph.Services {
			if s.Build != nil && !s.External {
				hasBuild = true
			}
		}
		if err := d.CheckExecution(app, hasBuild); err != nil {
			result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Field: "configuration", Message: err.Error()})
		}
	}
	if app.DeployTarget.Kind != "" {
		if err := CheckAppTarget(app); err != nil {
			result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Field: "target", Message: err.Error()})
		}
	}
	for _, s := range result.Graph.Services {
		if !s.External {
			result.Workloads[s.Name] = runtimeWorkloadName(app, s.Name)
		}
	}
	result.Deployable = len(result.Diagnostics) == 0 && app.DeployTarget.Kind != ""
	// Only a redacted copy crosses the preview boundary. Runtime values remain
	// in the short-lived resolved graph used by deployment.
	redactComposeGraph(result.Graph)
	return result, nil
}

type resolvedCompose struct {
	Project           *ct.Project
	Graph             *model.ComposeGraph
	BuildFiles        []BuildFilePreview
	Diagnostics       []PlanDiagnostic
	RequiredVariables []string
	Profiles          []string
	YAML              string
}

func loadRepositoryCompose(ctx context.Context, root string, app *model.App, env map[string]string) (*resolvedCompose, error) {
	plan := app.BuildPlan
	files := append([]string{}, plan.Files...)
	if len(files) == 0 {
		files = []string{plan.Path}
	}
	if len(files) == 0 || files[0] == "" {
		return nil, fmt.Errorf("select a Compose file")
	}
	values := ct.Mapping(mergeEnv(env, plan.Variables))
	if values == nil {
		values = ct.Mapping{}
	}
	configs := []ct.ConfigFile{}
	result := &resolvedCompose{BuildFiles: []BuildFilePreview{}, Diagnostics: []PlanDiagnostic{}, RequiredVariables: []string{}, Profiles: []string{}}
	variables := map[string]bool{}
	for _, file := range files {
		if !safeRelativePath(file) {
			return nil, fmt.Errorf("compose path leaves the repository")
		}
		path, err := repositoryPath(root, root, file)
		if err != nil {
			return nil, err
		}
		data, err := readSourceFile(path)
		if err != nil {
			return nil, err
		}
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("invalid YAML in %s", file)
		}
		if _, ok := raw["include"]; ok {
			return nil, fmt.Errorf("%s: include is not supported; select local files as ordered overrides", file)
		}
		if services, ok := raw["services"].(map[string]any); ok {
			for name, v := range services {
				if svc, ok := v.(map[string]any); ok {
					for _, key := range []string{"extends", "label_file"} {
						if _, present := svc[key]; present {
							return nil, fmt.Errorf("%s: service %s uses unsupported %s", file, name, key)
						}
					}
				}
			}
		}
		bare := bareComposeVariables(raw)
		for name, v := range template.ExtractVariables(raw, nil) {
			variables[name] = true
			if _, ok := values[name]; !ok && (v.Required || bare[name]) {
				result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Field: "variables", Message: "Provide Compose variable " + name + " in the selected environment or interpolation inputs"})
				// Keep a preview renderable while clearly blocking execution.
				values[name] = "COOKER_UNSET_" + name
			}
		}
		configs = append(configs, ct.ConfigFile{Filename: path, Content: data})
	}
	for name := range variables {
		result.RequiredVariables = append(result.RequiredVariables, name)
	}
	sort.Strings(result.RequiredVariables)
	prefix := AppPrefix(app)
	if prefix == "" {
		prefix = "preview"
	}
	project, err := loader.LoadWithContext(ctx, ct.ConfigDetails{WorkingDir: filepath.Dir(configs[0].Filename), ConfigFiles: configs, Environment: values}, func(o *loader.Options) {
		o.SetProjectName(prefix, true)
		o.ResolvePaths = true
		o.SkipInclude = true
		o.SkipExtends = true
		o.SkipResolveEnvironment = true
		o.Profiles = plan.Profiles
	})
	if err != nil {
		return nil, fmt.Errorf("compose validation failed: %s", maskValues(err.Error(), values))
	}
	profiles := map[string]bool{}
	for _, s := range project.AllServices() {
		for _, p := range s.Profiles {
			profiles[p] = true
		}
	}
	for p := range profiles {
		result.Profiles = append(result.Profiles, p)
	}
	sort.Strings(result.Profiles)
	for name, s := range project.Services {
		for i, ef := range s.EnvFiles {
			path, err := repositoryPath(root, project.WorkingDir, ef.Path)
			if err != nil {
				if !bool(ef.Required) && os.IsNotExist(sourceStatError(ef.Path)) {
					continue
				}
				return nil, fmt.Errorf("service %s has an unavailable or out-of-repository env_file", name)
			}
			if _, err := readSourceFile(path); err != nil {
				return nil, fmt.Errorf("service %s: cannot load env_file", name)
			}
			s.EnvFiles[i].Path = path
		}
		project.Services[name] = s
	}
	project, err = project.WithServicesEnvironmentResolved(true)
	if err != nil {
		return nil, fmt.Errorf("compose environment resolution failed")
	}
	graph := &model.ComposeGraph{Services: []model.ComposeService{}, Connections: []model.ComposeConnection{}, Networks: []string{}, Volumes: []string{}}
	for name := range project.Networks {
		graph.Networks = append(graph.Networks, name)
	}
	sort.Strings(graph.Networks)
	for name := range project.Volumes {
		graph.Volumes = append(graph.Volumes, name)
	}
	sort.Strings(graph.Volumes)
	external := map[string]model.ExternalServiceBinding{}
	for _, b := range app.DeployTarget.ExternalServices {
		external[b.Service] = b
	}
	names := project.ServiceNames()
	sort.Strings(names)
	workloads := map[string]bool{}
	for _, name := range names {
		s := project.Services[name]
		gs := model.ComposeService{Name: name, Image: s.Image, Ports: []string{}, Environment: map[string]string{}, DependsOn: []string{}, Networks: []string{}, Volumes: []string{}, Status: "unknown", Labels: s.Labels, Command: strings.Join(s.Command, " "), CommandArgs: []string(s.Command)}
		if s.HealthCheck != nil && !s.HealthCheck.Disable && len(s.HealthCheck.Test) > 0 {
			h := s.HealthCheck
			gs.HealthCheck = &model.ContainerHealthCheck{Command: []string(h.Test), Interval: 30, Timeout: 5, Retries: 3}
			if h.Interval != nil {
				gs.HealthCheck.Interval = int32(time.Duration(*h.Interval) / time.Second)
			}
			if h.Timeout != nil {
				gs.HealthCheck.Timeout = int32(time.Duration(*h.Timeout) / time.Second)
			}
			if h.Retries != nil {
				gs.HealthCheck.Retries = int32(*h.Retries)
			}
			if h.StartPeriod != nil {
				gs.HealthCheck.StartPeriod = int32(time.Duration(*h.StartPeriod) / time.Second)
			}
		}
		_, gs.External = external[name]
		for k, v := range s.Environment {
			if v != nil {
				gs.Environment[k] = *v
			}
		}
		for dep := range s.DependsOn {
			gs.DependsOn = append(gs.DependsOn, dep)
			graph.Connections = append(graph.Connections, model.ComposeConnection{Source: name, Target: dep, Type: "depends_on", Label: "depends_on"})
		}
		sort.Strings(gs.DependsOn)
		for network := range s.Networks {
			gs.Networks = append(gs.Networks, network)
		}
		sort.Strings(gs.Networks)
		for _, v := range s.Volumes {
			gs.Volumes = append(gs.Volumes, v.Source+":"+v.Target)
		}
		for _, p := range s.Ports {
			value := fmt.Sprint(p.Target)
			if p.Published != "" {
				value = p.Published + ":" + value
			}
			if p.Protocol != "" && p.Protocol != "tcp" {
				value += "/" + p.Protocol
			}
			gs.Ports = append(gs.Ports, value)
		}
		gs.Group = deriveGroup(gs.Labels, gs.Networks)
		memory, cpus := int64(s.MemLimit), float64(s.CPUS)
		if s.Deploy != nil && s.Deploy.Resources.Limits != nil {
			if s.Deploy.Resources.Limits.MemoryBytes != 0 {
				memory = int64(s.Deploy.Resources.Limits.MemoryBytes)
			}
			if s.Deploy.Resources.Limits.NanoCPUs != 0 {
				cpus = float64(s.Deploy.Resources.Limits.NanoCPUs)
			}
		}
		if memory != 0 || cpus != 0 {
			gs.Resources = &model.ResourceLimits{MemoryBytes: memory, NanoCPUs: int64(cpus * 1e9)}
			if memory != 0 {
				gs.Resources.Memory = fmt.Sprint(memory)
			}
			if cpus != 0 {
				gs.Resources.CPUs = fmt.Sprint(cpus)
			}
		}
		if !gs.External {
			if s.Image != "" {
				if _, err := reference.ParseNormalizedNamed(s.Image); err != nil {
					result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Service: name, Field: "image", Message: "Image must be a valid container image reference"})
				}
			}
			if app.DeployTarget.Kind == model.DeployTargetECS {
				cpu, memory, err := ecs.TaskSize(gs.Resources)
				if err != nil {
					result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Service: name, Field: "resources", Message: err.Error()})
				} else {
					gs.RuntimeSize = fmt.Sprintf("Fargate: %.2g vCPU, %d MiB", float64(cpu)/1024, memory)
				}
				if h := gs.HealthCheck; h != nil && (h.Interval < 5 || h.Interval > 300 || h.Timeout < 2 || h.Timeout > 60 || h.Retries < 1 || h.Retries > 10 || h.StartPeriod < 0 || h.StartPeriod > 300) {
					result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Service: name, Field: "healthcheck", Message: "ECS healthcheck needs interval 5–300s, timeout 2–60s, retries 1–10 and start period 0–300s"})
				}
			}
			identity := RuntimeServiceName(app, name)
			if !namespacePattern.MatchString(identity) || workloads[identity] {
				result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Service: name, Field: "prefix", Message: "Generated workload name is invalid, too long or collides; choose another prefix or service name"})
			}
			if app.DeployTarget.Kind == model.DeployTargetCloudRun && (len(identity) >= 50 || identity[0] < 'a' || identity[0] > 'z') {
				result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Service: name, Field: "prefix", Message: "Cloud Run workload names must start with a letter and contain fewer than 50 characters"})
			}
			workloads[identity] = true
			result.Diagnostics = append(result.Diagnostics, composeCompatibility(s, app.DeployTarget.Kind, external)...)
			result.Diagnostics = append(result.Diagnostics, composeResourceCompatibility(s, project, app.DeployTarget.Kind)...)
			if s.Build != nil {
				contextPath, err := repositoryPath(root, project.WorkingDir, s.Build.Context)
				if err != nil {
					return nil, fmt.Errorf("service %s: %w", name, err)
				}
				filePath, err := repositoryPath(root, contextPath, s.Build.Dockerfile)
				if err != nil {
					return nil, fmt.Errorf("service %s: %w", name, err)
				}
				content, err := readSourceFile(filePath)
				if err != nil {
					return nil, fmt.Errorf("service %s: cannot read Dockerfile", name)
				}
				relContext, _ := filepath.Rel(root, contextPath)
				relDockerfile, _ := filepath.Rel(contextPath, filePath)
				relFile, _ := filepath.Rel(root, filePath)
				gs.Build = &model.ComposeBuild{Context: filepath.ToSlash(relContext), Dockerfile: filepath.ToSlash(relDockerfile), Args: map[string]string{}}
				gs.Build.Target = s.Build.Target
				for k, v := range s.Build.Args {
					if v != nil {
						gs.Build.Args[k] = *v
					} else if value, ok := values[k]; ok {
						gs.Build.Args[k] = value
					} else {
						result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Service: name, Field: "build.args", Message: "Provide build argument " + k})
					}
				}
				result.BuildFiles = append(result.BuildFiles, BuildFilePreview{Service: name, Context: gs.Build.Context, Dockerfile: filepath.ToSlash(relFile), Content: maskDockerfile(string(content))})
			}
		}
		graph.Services = append(graph.Services, gs)
	}
	if len(graph.Services) == 0 {
		return nil, fmt.Errorf("selected files and profiles contain no services")
	}
	if err := bindExternalServices(graph, app, env); err != nil {
		result.Diagnostics = append(result.Diagnostics, PlanDiagnostic{Field: "externalServices", Message: err.Error()})
	}
	// A safe, portable summary of exactly the runtime inputs Cooker understands.
	previewData, _ := json.Marshal(graph)
	var safe model.ComposeGraph
	_ = json.Unmarshal(previewData, &safe)
	redactComposeGraph(&safe)
	yamlData, _ := yaml.Marshal(map[string]any{"name": prefix, "services": safe.Services, "networks": safe.Networks, "volumes": safe.Volumes})
	result.YAML = string(yamlData)
	result.Graph = graph
	result.Project = project
	return result, nil
}

func sourceStatError(path string) error { _, err := os.Stat(path); return err }

func composeCompatibility(s ct.ServiceConfig, kind model.DeployTargetKind, external map[string]model.ExternalServiceBinding) []PlanDiagnostic {
	var ds []PlanDiagnostic
	add := func(field, message string) {
		ds = append(ds, PlanDiagnostic{Service: s.Name, Field: field, Message: message})
	}
	native := kind == model.DeployTargetDockerHost
	if len(s.Volumes) > 0 && !native {
		add("volumes", "Volume deployment is not supported by this per-service target; use an existing database binding or remove the volume")
	}
	if native {
		for _, v := range s.Volumes {
			if v.Type != "volume" && v.Type != "tmpfs" {
				add("volumes", "Only named volumes and tmpfs are supported; repository bind mounts would disappear after deployment")
			}
		}
	}
	if len(s.Secrets) > 0 || len(s.Configs) > 0 {
		add("secrets/configs", "Use the linked Cooker environment for runtime values; Compose file mounts are not supported")
	}
	if s.ContainerName != "" {
		add("container_name", "Remove container_name so the deployment prefix can isolate this service")
	}
	if s.HealthCheck != nil && !native && kind != model.DeployTargetECS {
		add("healthcheck", "Compose healthcheck translation is currently supported for ECS; put health behavior in the image for this target")
	}
	if s.HealthCheck != nil && s.HealthCheck.StartInterval != nil && kind == model.DeployTargetECS {
		add("healthcheck.start_interval", "ECS does not support start_interval")
	}
	if len(s.Command) > 0 && !native && kind != model.DeployTargetECS && kind != model.DeployTargetCloudRun {
		add("command", "Put the runtime command in the Dockerfile for this target")
	}
	if !native && (len(s.Entrypoint) > 0 || s.User != "" || s.WorkingDir != "") {
		add("entrypoint/user/working_dir", "Put entrypoint, user and working directory in the Dockerfile for this target")
	}
	if s.Privileged || len(s.CapAdd) > 0 || len(s.Devices) > 0 || len(s.Gpus) > 0 || s.NetworkMode != "" || s.Pid != "" {
		add("runtime", "Host privileges, devices and namespace overrides are not supported")
	}
	for name, d := range s.DependsOn {
		if _, ok := external[name]; ok {
			continue
		}
		if d.Condition == "service_completed_successfully" || !native && d.Condition != "" && d.Condition != "service_started" {
			add("depends_on", "Health/completion dependency conditions are not translated by this target")
		}
		if !native {
			add("depends_on", "Cross-service discovery requires an external endpoint binding on this target; Docker Compose provides native service DNS")
		}
	}
	for _, p := range s.Ports {
		if !native && (p.Protocol != "" && p.Protocol != "tcp" || p.HostIP != "" || strings.Contains(p.Published, "-")) {
			add("ports", "Only TCP ports without host-IP binding or ranges are supported")
		}
	}
	if s.Build != nil {
		b := s.Build
		if b.DockerfileInline != "" || len(b.AdditionalContexts) > 0 || len(b.Secrets) > 0 || len(b.SSH) > 0 || len(b.Platforms) > 0 {
			add("build", "Inline Dockerfiles, extra contexts, build secrets/SSH and platform overrides are not supported by this deployment path")
		}
		if s.Image != "" && s.PullPolicy != "build" {
			add("pull_policy", "This deployment builds and pushes source images; set pull_policy: build when both image and build are specified")
		}
	}
	if kind == model.DeployTargetCloudRun && (len(s.Ports) != 1 || s.Ports[0].Target != 8080) {
		add("ports", "This Cloud Run adapter requires one container port at 8080")
	}
	return ds
}

// ExtractVariables does not distinguish an empty default from no default.
// Inspect complete template matches to keep ${NAME:-} and $$NAME optional.
func bareComposeVariables(value any) map[string]bool {
	out := map[string]bool{}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case string:
			for _, m := range template.DefaultPattern.FindAllString(v, -1) {
				if strings.HasPrefix(m, "$$") {
					continue
				}
				name := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(m, "$"), "{"), "}")
				if variablePattern.MatchString(name) {
					out[name] = true
				}
			}
		case map[string]any:
			for _, item := range v {
				walk(item)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(value)
	return out
}

func bindExternalServices(graph *model.ComposeGraph, app *model.App, env map[string]string) error {
	services := map[string]*model.ComposeService{}
	for i := range graph.Services {
		services[graph.Services[i].Name] = &graph.Services[i]
	}
	for _, binding := range app.DeployTarget.ExternalServices {
		service, ok := services[binding.Service]
		if !ok {
			return fmt.Errorf("external service %s is not in the selected Compose project", binding.Service)
		}
		service.External = true
		consumers := map[string]bool{}
		for _, name := range binding.Consumers {
			consumer, ok := services[name]
			if !ok || consumer.External || name == binding.Service {
				return fmt.Errorf("invalid consumer %s for %s", name, binding.Service)
			}
			consumers[name] = true
			for target, source := range binding.Environment {
				value, ok := env[source]
				if !ok || value == "" {
					return fmt.Errorf("environment key %s is required for %s", source, binding.Service)
				}
				consumer.Environment[target] = value
			}
			graph.Connections = append(graph.Connections, model.ComposeConnection{Source: name, Target: binding.Service, Type: "env_reference", Label: "external database"})
		}
		for _, s := range graph.Services {
			for _, dep := range s.DependsOn {
				if dep == binding.Service && !consumers[s.Name] {
					return fmt.Errorf("service %s depends on %s; add it as a binding consumer", s.Name, binding.Service)
				}
			}
		}
	}
	return nil
}

func redactComposeGraph(graph *model.ComposeGraph) {
	for i := range graph.Services {
		s := &graph.Services[i]
		if s.Command != "" {
			s.Command = "[configured]"
		}
		s.CommandArgs = nil
		s.Labels = nil
		if s.HealthCheck != nil {
			s.HealthCheck.Command = []string{"[configured]"}
		}
		for k := range s.Environment {
			s.Environment[k] = "[configured]"
		}
		if s.Build != nil {
			for k := range s.Build.Args {
				s.Build.Args[k] = "[configured]"
			}
		}
	}
}
func maskValues(message string, values map[string]string) string {
	for _, v := range values {
		if len(v) > 2 {
			message = strings.ReplaceAll(message, v, "[configured]")
		}
	}
	return message
}
func maskDockerfile(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		upper := strings.ToUpper(trim)
		if strings.HasPrefix(upper, "ENV ") || strings.HasPrefix(upper, "ARG ") {
			fields := strings.Fields(trim)
			if len(fields) > 1 {
				key := strings.SplitN(fields[1], "=", 2)[0]
				lines[i] = fields[0] + " " + key + " # value hidden in preview"
			}
		}
	}
	return strings.Join(lines, "\n")
}
