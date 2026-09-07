package service

import (
	"fmt"
	"os"
	"strings"

	ct "github.com/compose-spec/compose-go/v2/types"
	"github.com/santapong/cooker/internal/model"
	"gopkg.in/yaml.v3"
)

// writeRuntimeCompose preserves Docker networking, service DNS and named
// volumes. Only reviewed images run; Compose never builds a second time.
// The private file lives outside the build context and is removed by the caller.
func writeRuntimeCompose(resolved *resolvedCompose, app *model.App, root, registry string, ts int64, opts synthOpts) (string, error) {
	p := resolved.Project
	for _, svc := range resolved.Graph.Services {
		if svc.External {
			delete(p.Services, svc.Name)
			continue
		}
		s := p.Services[svc.Name]
		if svc.Build != nil {
			s.Image = fmt.Sprintf("%s/%s:%d", registry, RuntimeServiceName(app, svc.Name), ts)
			s.PullPolicy = "missing"
		}
		s.Build = nil
		s.Profiles = nil
		s.Environment = ct.MappingWithEquals{}
		for k, v := range mergeEnv(opts.appEnv, svc.Environment) {
			value := v
			s.Environment[k] = &value
		}
		for _, binding := range app.DeployTarget.ExternalServices {
			delete(s.DependsOn, binding.Service)
		}
		if host := opts.proxy.hostFor(RuntimeServiceName(app, svc.Name)); host != "" {
			if s.Labels == nil {
				s.Labels = ct.Labels{}
			}
			for k, v := range traefikLabels(RuntimeServiceName(app, svc.Name), host, firstContainerPort(svc.Ports)) {
				s.Labels[k] = v
			}
			if opts.proxy.Network != "" {
				p.Networks["cooker_proxy"] = ct.NetworkConfig{Name: opts.proxy.Network, External: true}
				if s.Networks == nil {
					s.Networks = map[string]*ct.ServiceNetworkConfig{}
				}
				s.Networks["cooker_proxy"] = nil
			}
		}
		p.Services[svc.Name] = s
	}
	usedVolumes, usedNetworks := map[string]bool{}, map[string]bool{}
	for _, s := range p.Services {
		for _, v := range s.Volumes {
			if v.Type == "volume" {
				usedVolumes[v.Source] = true
			}
		}
		for n := range s.Networks {
			usedNetworks[n] = true
		}
	}
	for n := range p.Volumes {
		if !usedVolumes[n] {
			delete(p.Volumes, n)
		}
	}
	for n := range p.Networks {
		if !usedNetworks[n] {
			delete(p.Networks, n)
		}
	}
	p.Secrets = nil
	p.Configs = nil
	data, err := p.MarshalYAML()
	if err != nil {
		return "", fmt.Errorf("render runtime Compose: %w", err)
	}
	// Environment/commands were interpolated once during review. Escaping $
	// prevents the Docker CLI from substituting values from its host process.
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	data, err = yaml.Marshal(escapeComposeValues(doc))
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "cooker-runtime-*.yaml")
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(f.Name())
		return "", writeErr
	}
	if closeErr != nil {
		_ = os.Remove(f.Name())
		return "", closeErr
	}
	return f.Name(), nil
}

func escapeComposeValues(value any) any {
	switch v := value.(type) {
	case string:
		return strings.ReplaceAll(v, "$", "$$")
	case map[string]any:
		for k, item := range v {
			v[k] = escapeComposeValues(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = escapeComposeValues(item)
		}
		return v
	default:
		return value
	}
}
