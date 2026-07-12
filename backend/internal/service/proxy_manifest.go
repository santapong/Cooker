package service

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// envYAMLBlock renders a Kubernetes container `env:` list for the given
// map, each line prefixed with indent, keys sorted for determinism.
// Values go through yaml.Marshal so arbitrary secret content (quotes,
// newlines, colons) cannot break or inject into the synthesized
// manifest. Returns "" for an empty map.
func envYAMLBlock(env map[string]string, indent string) string {
	if len(env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type kv struct {
		Name  string `yaml:"name"`
		Value string `yaml:"value"`
	}
	list := make([]kv, 0, len(keys))
	for _, k := range keys {
		list = append(list, kv{Name: k, Value: env[k]})
	}
	raw, err := yaml.Marshal(list)
	if err != nil {
		// Marshal of a []struct{string,string} cannot realistically
		// fail; guard anyway rather than emit a broken manifest.
		return ""
	}
	var b strings.Builder
	b.WriteString(indent + "env:\n")
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		b.WriteString(indent + "  " + line + "\n")
	}
	return b.String()
}

// ingressYAML synthesizes a networking.k8s.io/v1 Ingress document
// routing host → service:port. Appended (with a `---` separator) to
// the Deployment+Service manifests when the deployed-app proxy is
// configured. ingressClass empty omits ingressClassName (cluster
// default class handles it).
func ingressYAML(name, host, serviceName string, port int, ingressClass string) string {
	classLine := ""
	if ingressClass != "" {
		classLine = fmt.Sprintf("  ingressClassName: %s\n", ingressClass)
	}
	return fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: %[1]s
spec:
%[5]s  rules:
    - host: %[2]s
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: %[3]s
                port: {number: %[4]d}
`, name, host, serviceName, port, classLine)
}

// traefikLabels builds the container labels a docker-run deploy needs
// for Traefik to route host → container port. router is a unique,
// DNS-safe router name (the stage's service slug).
func traefikLabels(router, host string, port int) map[string]string {
	return map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", router):                      fmt.Sprintf("Host(`%s`)", host),
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", router): fmt.Sprintf("%d", port),
	}
}
