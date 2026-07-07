package service

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/santapong/cooker/internal/model"
)

// defaultKubernetesManifest synthesises a minimal Deployment +
// Service so UAT can click Deploy on an App pointed at a Kubernetes
// target without first writing YAML. Real workloads override this
// via App.BuildPlan / a custom pipeline.
func defaultKubernetesManifest(app *model.App, image string) string {
	name := sanitize(app.Name)
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %[1]s
spec:
  replicas: 1
  selector:
    matchLabels: {app: %[1]s}
  template:
    metadata:
      labels: {app: %[1]s}
    spec:
      containers:
        - name: %[1]s
          image: %[2]s
          ports: [{containerPort: 80}]
---
apiVersion: v1
kind: Service
metadata:
  name: %[1]s
spec:
  selector: {app: %[1]s}
  ports: [{port: 80, targetPort: 80}]
`, name, image)
}

// deployRuntime selects how a synthesized deploy stage runs.
type deployRuntime string

const (
	deployRuntimeKubernetes deployRuntime = "kubernetes"
	deployRuntimeDocker     deployRuntime = "docker"
	deployRuntimeCompose    deployRuntime = "compose"
)

// deployRuntimeFor maps an App's deploy-target kind to the per-service
// deploy runtime. Kubernetes → manifest apply; docker-host → per-
// service docker run. Other targets default to kubernetes-manifest
// semantics for now.
func deployRuntimeFor(kind model.DeployTargetKind) deployRuntime {
	switch kind {
	case model.DeployTargetDockerHost:
		return deployRuntimeDocker
	default:
		return deployRuntimeKubernetes
	}
}

// composeServiceManifest synthesises a minimal Deployment + Service for
// one compose service, parameterised by image, first published port,
// and (optionally) resource limits. Mirrors defaultKubernetesManifest
// but per-service and resource-aware.
func composeServiceManifest(svc *model.ComposeService, image string) string {
	name := sanitize(svc.Name)
	port := firstContainerPort(svc.Ports)
	resources := k8sResourceBlock(svc.Resources)
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %[1]s
spec:
  replicas: 1
  selector:
    matchLabels: {app: %[1]s}
  template:
    metadata:
      labels: {app: %[1]s}
    spec:
      containers:
        - name: %[1]s
          image: %[2]s
          ports: [{containerPort: %[3]d}]%[4]s
---
apiVersion: v1
kind: Service
metadata:
  name: %[1]s
spec:
  selector: {app: %[1]s}
  ports: [{port: %[3]d, targetPort: %[3]d}]
`, name, image, port, resources)
}

// k8sResourceBlock renders a resources.limits YAML fragment (indented
// to sit under the container) or "" when no limits are set.
func k8sResourceBlock(r *model.ResourceLimits) string {
	if r == nil || (r.Memory == "" && r.CPUs == "") {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n          resources:\n            limits:")
	if r.Memory != "" {
		b.WriteString(fmt.Sprintf("\n              memory: %q", k8sMemory(r.Memory)))
	}
	if r.CPUs != "" {
		b.WriteString(fmt.Sprintf("\n              cpu: %q", r.CPUs))
	}
	return b.String()
}

// k8sMemory maps a compose memory string to a K8s quantity. Compose
// uses b/k/m/g; K8s uses Ki/Mi/Gi. Bare numbers pass through.
func k8sMemory(s string) string {
	low := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasSuffix(low, "gb"), strings.HasSuffix(low, "g"):
		return strings.TrimSuffix(strings.TrimSuffix(low, "b"), "g") + "Gi"
	case strings.HasSuffix(low, "mb"), strings.HasSuffix(low, "m"):
		return strings.TrimSuffix(strings.TrimSuffix(low, "b"), "m") + "Mi"
	case strings.HasSuffix(low, "kb"), strings.HasSuffix(low, "k"):
		return strings.TrimSuffix(strings.TrimSuffix(low, "b"), "k") + "Ki"
	default:
		return s
	}
}

// firstContainerPort parses the container side of the first compose
// port mapping ("8080:80" → 80, "80" → 80). Defaults to 80.
func firstContainerPort(ports []string) int {
	for _, p := range ports {
		spec := p
		if i := strings.LastIndex(p, ":"); i >= 0 {
			spec = p[i+1:]
		}
		spec = strings.SplitN(spec, "/", 2)[0] // strip "/tcp"
		if n, err := strconv.Atoi(strings.TrimSpace(spec)); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	return 80
}

// sanitize returns a DNS-1123 safe slug of s. The rules here are
// intentionally conservative — we replace anything that isn't
// lowercase alphanumeric with a dash.
func sanitize(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

// fanOut returns a writer that multiplexes w and secondary. Nil
// values are skipped. Concurrent-safe only when both inputs are.
func fanOut(w, secondary io.Writer) io.Writer {
	switch {
	case w != nil && secondary != nil:
		return &mwWriter{a: w, b: secondary}
	case w != nil:
		return w
	case secondary != nil:
		return secondary
	}
	return io.Discard
}

type mwWriter struct {
	mu   sync.Mutex
	a, b io.Writer
}

func (m *mwWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.a.Write(p)
	_, _ = m.b.Write(p)
	return len(p), nil
}
