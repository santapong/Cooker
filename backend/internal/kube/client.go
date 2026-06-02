// Package kube provides a read-only, typed client-go wrapper used by the
// HTTP layer to list and inspect Kubernetes resources against the
// server's configured cluster. It is deliberately read-only: no create,
// scale, restart, or delete lives here. The write path (scale/restart/
// apply/delete) flows through the deployer adapters and is tracked
// separately.
//
// The rest.Config logic mirrors internal/deployer/clientgo.go restConfig
// so the inspect endpoints authenticate to exactly the same cluster the
// pipeline deployer targets. Crucially, the kubeconfig source is the
// server's own configuration (COOKER_KUBECONFIG / in-cluster) — never a
// user-supplied path or URL — so these endpoints cannot be steered at an
// arbitrary cluster (SSRF).
package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/santapong/cooker/internal/model"
)

// ErrUnavailable is returned when the cluster client cannot be built —
// no kubeconfig, no in-cluster config, or a bad rest.Config. Handlers
// translate this (via errors.Is) into a 503 so callers can tell "cluster
// not configured" apart from a genuine 500.
var ErrUnavailable = errors.New("kube: unavailable")

// logCapBytes hard-caps the bytes read from a pod log stream. A pod can
// emit gigabytes of logs; without a cap a single GetPodLogs call could
// exhaust server memory. 256 KiB is far more than tailLines<=10000 of
// realistic log lines and bounds the worst case regardless.
const logCapBytes = 256 << 10

const (
	defaultTailLines = 1000
	minTailLines     = 1
	maxTailLines     = 10000
)

// Client is a read-only Kubernetes client. Construct with New for
// production (lazy real init against the server's kubeconfig) or
// NewWithClientset for tests (inject a fake clientset).
type Client struct {
	kubeconfig string // empty means in-cluster, then default loading rules

	// initOnce serialises lazy clientset construction across concurrent
	// callers; initErr captures the one-shot result. cs holds the typed
	// clientset once built.
	initOnce sync.Once
	initErr  error
	cs       kubernetes.Interface

	// injected, when non-nil, is returned by clientset() without any
	// lazy init. Set only by NewWithClientset for tests.
	injected kubernetes.Interface
}

// New returns a Client that lazily builds a real clientset from the
// given kubeconfig (empty => in-cluster, then default loading rules).
// Construction never touches the cluster, so a server with no cluster
// configured still boots; the first read returns ErrUnavailable.
func New(kubeconfig string) *Client { return &Client{kubeconfig: kubeconfig} }

// NewWithClientset returns a Client backed by an already-built clientset.
// Intended for tests with k8s.io/client-go/kubernetes/fake.
func NewWithClientset(cs kubernetes.Interface) *Client {
	return &Client{injected: cs}
}

// restConfig mirrors deployer.ClientGo.restConfig: an empty kubeconfig
// tries in-cluster first and falls through to the default loading rules
// (so a workstation with ~/.kube/config works); a non-empty kubeconfig
// is loaded via ExplicitPath. The path comes from server config only.
func (c *Client) restConfig() (*rest.Config, error) {
	if c.kubeconfig == "" {
		cfg, err := rest.InClusterConfig()
		if err == nil {
			return cfg, nil
		}
		// Fall through to default loading rules — works on a workstation.
	}
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	if c.kubeconfig != "" {
		loader.ExplicitPath = c.kubeconfig
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{}).ClientConfig()
}

// clientset returns the injected clientset if present, else lazily builds
// the real one. Init failures are wrapped as ErrUnavailable.
func (c *Client) clientset() (kubernetes.Interface, error) {
	if c.injected != nil {
		return c.injected, nil
	}
	c.initOnce.Do(func() {
		cfg, err := c.restConfig()
		if err != nil {
			c.initErr = fmt.Errorf("%w: rest config: %v", ErrUnavailable, err)
			return
		}
		cs, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			c.initErr = fmt.Errorf("%w: clientset: %v", ErrUnavailable, err)
			return
		}
		c.cs = cs
	})
	if c.initErr != nil {
		return nil, c.initErr
	}
	return c.cs, nil
}

// ListNamespaces lists all namespaces, mapping Name/Status/Labels.
func (c *Client) ListNamespaces(ctx context.Context) ([]model.KubeNamespace, error) {
	cs, err := c.clientset()
	if err != nil {
		return nil, err
	}
	list, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("kube: list namespaces: %w", err)
	}
	out := make([]model.KubeNamespace, 0, len(list.Items))
	for i := range list.Items {
		ns := &list.Items[i]
		out = append(out, model.KubeNamespace{
			Name:   ns.Name,
			Status: string(ns.Status.Phase),
			Labels: ns.Labels,
		})
	}
	return out, nil
}

// ListWorkloads lists Deployments, StatefulSets, and DaemonSets in the
// given namespace. An empty namespace lists across all namespaces.
func (c *Client) ListWorkloads(ctx context.Context, namespace string) ([]model.KubeWorkload, error) {
	cs, err := c.clientset()
	if err != nil {
		return nil, err
	}
	ns := namespace
	if ns == "" {
		ns = metav1.NamespaceAll
	}

	var out []model.KubeWorkload

	deps, err := cs.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("kube: list deployments: %w", err)
	}
	for i := range deps.Items {
		out = append(out, deploymentToWorkload(&deps.Items[i]))
	}

	sts, err := cs.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("kube: list statefulsets: %w", err)
	}
	for i := range sts.Items {
		out = append(out, statefulSetToWorkload(&sts.Items[i]))
	}

	ds, err := cs.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("kube: list daemonsets: %w", err)
	}
	for i := range ds.Items {
		out = append(out, daemonSetToWorkload(&ds.Items[i]))
	}

	if out == nil {
		out = []model.KubeWorkload{}
	}
	return out, nil
}

// GetWorkload fetches a single workload by kind (case-insensitive:
// deployment/statefulset/daemonset). Unknown kinds return a clear error.
func (c *Client) GetWorkload(ctx context.Context, namespace, kind, name string) (model.KubeWorkload, error) {
	cs, err := c.clientset()
	if err != nil {
		return model.KubeWorkload{}, err
	}
	switch strings.ToLower(kind) {
	case "deployment", "deployments":
		d, err := cs.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return model.KubeWorkload{}, fmt.Errorf("kube: get deployment: %w", err)
		}
		return deploymentToWorkload(d), nil
	case "statefulset", "statefulsets":
		s, err := cs.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return model.KubeWorkload{}, fmt.Errorf("kube: get statefulset: %w", err)
		}
		return statefulSetToWorkload(s), nil
	case "daemonset", "daemonsets":
		d, err := cs.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return model.KubeWorkload{}, fmt.Errorf("kube: get daemonset: %w", err)
		}
		return daemonSetToWorkload(d), nil
	default:
		return model.KubeWorkload{}, fmt.Errorf("kube: get workload: unknown kind %q (want deployment, statefulset, or daemonset)", kind)
	}
}

// GetPodLogs streams the tail of a pod's logs into a capped buffer.
// tailLines defaults to 1000 and is clamped to [1, 10000]. At most
// logCapBytes are read regardless, so a huge log can't exhaust memory.
func (c *Client) GetPodLogs(ctx context.Context, namespace, name string, tailLines int64) (string, error) {
	cs, err := c.clientset()
	if err != nil {
		return "", err
	}
	if tailLines <= 0 {
		tailLines = defaultTailLines
	}
	if tailLines < minTailLines {
		tailLines = minTailLines
	}
	if tailLines > maxTailLines {
		tailLines = maxTailLines
	}

	req := cs.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{TailLines: &tailLines})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("kube: pod logs: %w", err)
	}
	defer func() { _ = stream.Close() }()

	var buf bytes.Buffer
	// LimitReader caps reads at logCapBytes+1 so we can tell whether the
	// stream was truncated and append a marker rather than silently
	// dropping the tail.
	if _, err := io.Copy(&buf, io.LimitReader(stream, logCapBytes+1)); err != nil {
		return "", fmt.Errorf("kube: read pod logs: %w", err)
	}
	if buf.Len() > logCapBytes {
		out := buf.Bytes()[:logCapBytes]
		return string(out) + "\n...[truncated]\n", nil
	}
	return buf.String(), nil
}

// deploymentToWorkload maps an appsv1.Deployment to the API model.
func deploymentToWorkload(d *appsv1.Deployment) model.KubeWorkload {
	var replicas int32
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	return model.KubeWorkload{
		Kind:      "Deployment",
		Name:      d.Name,
		Namespace: d.Namespace,
		Replicas:  replicas,
		Ready:     d.Status.ReadyReplicas,
		Image:     firstContainerImage(d.Spec.Template.Spec.Containers),
		Status:    workloadStatus(replicas, d.Status.ReadyReplicas),
	}
}

// statefulSetToWorkload maps an appsv1.StatefulSet to the API model.
func statefulSetToWorkload(s *appsv1.StatefulSet) model.KubeWorkload {
	var replicas int32
	if s.Spec.Replicas != nil {
		replicas = *s.Spec.Replicas
	}
	return model.KubeWorkload{
		Kind:      "StatefulSet",
		Name:      s.Name,
		Namespace: s.Namespace,
		Replicas:  replicas,
		Ready:     s.Status.ReadyReplicas,
		Image:     firstContainerImage(s.Spec.Template.Spec.Containers),
		Status:    workloadStatus(replicas, s.Status.ReadyReplicas),
	}
}

// daemonSetToWorkload maps an appsv1.DaemonSet to the API model. A
// DaemonSet has no Spec.Replicas; desired/ready come from status.
func daemonSetToWorkload(d *appsv1.DaemonSet) model.KubeWorkload {
	desired := d.Status.DesiredNumberScheduled
	ready := d.Status.NumberReady
	return model.KubeWorkload{
		Kind:      "DaemonSet",
		Name:      d.Name,
		Namespace: d.Namespace,
		Replicas:  desired,
		Ready:     ready,
		Image:     firstContainerImage(d.Spec.Template.Spec.Containers),
		Status:    workloadStatus(desired, ready),
	}
}

// firstContainerImage returns the image of the first container, or "".
func firstContainerImage(containers []corev1.Container) string {
	if len(containers) == 0 {
		return ""
	}
	return containers[0].Image
}

// workloadStatus derives a coarse human-readable status from the desired
// and ready replica counts.
func workloadStatus(desired, ready int32) string {
	switch {
	case desired == 0:
		return "Scaled to zero"
	case ready >= desired:
		return "Running"
	default:
		return "Progressing"
	}
}
