package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func int32Ptr(i int32) *int32 { return &i }

// seededClient returns a Client backed by a fake clientset holding one
// Deployment, one StatefulSet, one DaemonSet, one Namespace, and one Pod.
func seededClient() *Client {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "web", Image: "nginx:1.25"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 2},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "db", Image: "postgres:16"}},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: 1},
	}
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "agent", Image: "agent:v1"}},
				},
			},
		},
		Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 2},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"team": "core"}},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-abc", Namespace: "default"},
	}
	cs := fake.NewSimpleClientset(dep, sts, ds, ns, pod)
	return NewWithClientset(cs)
}

func TestListWorkloads_MapsAllKinds(t *testing.T) {
	c := seededClient()
	got, err := c.ListWorkloads(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListWorkloads: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 workloads, got %d: %+v", len(got), got)
	}
	byKind := map[string]struct {
		name            string
		image           string
		replicas, ready int32
	}{}
	for _, w := range got {
		byKind[w.Kind] = struct {
			name            string
			image           string
			replicas, ready int32
		}{w.Name, w.Image, w.Replicas, w.Ready}
	}

	dep, ok := byKind["Deployment"]
	if !ok {
		t.Fatalf("missing Deployment in %+v", got)
	}
	if dep.name != "web" || dep.image != "nginx:1.25" || dep.replicas != 3 || dep.ready != 2 {
		t.Errorf("Deployment mapped wrong: %+v", dep)
	}

	sts, ok := byKind["StatefulSet"]
	if !ok {
		t.Fatalf("missing StatefulSet in %+v", got)
	}
	if sts.name != "db" || sts.image != "postgres:16" || sts.replicas != 1 || sts.ready != 1 {
		t.Errorf("StatefulSet mapped wrong: %+v", sts)
	}

	ds, ok := byKind["DaemonSet"]
	if !ok {
		t.Fatalf("missing DaemonSet in %+v", got)
	}
	if ds.name != "agent" || ds.image != "agent:v1" || ds.replicas != 2 || ds.ready != 2 {
		t.Errorf("DaemonSet mapped wrong: %+v", ds)
	}
}

func TestGetWorkload_ByKind(t *testing.T) {
	c := seededClient()
	for _, kind := range []string{"deployment", "Deployment", "DEPLOYMENT"} {
		w, err := c.GetWorkload(context.Background(), "default", kind, "web")
		if err != nil {
			t.Fatalf("GetWorkload(%q): %v", kind, err)
		}
		if w.Kind != "Deployment" || w.Name != "web" || w.Image != "nginx:1.25" {
			t.Errorf("GetWorkload(%q) wrong: %+v", kind, w)
		}
	}

	if _, err := c.GetWorkload(context.Background(), "default", "cronjob", "web"); err == nil {
		t.Error("expected error for unknown kind, got nil")
	}
}

func TestListNamespaces_MapsStatus(t *testing.T) {
	c := seededClient()
	got, err := c.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 namespace, got %d: %+v", len(got), got)
	}
	if got[0].Name != "default" || got[0].Status != "Active" {
		t.Errorf("namespace mapped wrong: %+v", got[0])
	}
	if got[0].Labels["team"] != "core" {
		t.Errorf("expected label team=core, got %+v", got[0].Labels)
	}
}

func TestGetPodLogs_ReturnsStream(t *testing.T) {
	c := seededClient()
	// The fake clientset returns a canned "fake logs" body for GetLogs.
	logs, err := c.GetPodLogs(context.Background(), "default", "web-abc", 0)
	if err != nil {
		t.Fatalf("GetPodLogs: %v", err)
	}
	if !strings.Contains(logs, "fake logs") {
		t.Errorf("expected fake logs body, got %q", logs)
	}
}

func TestGetPodLogs_ClampsTailLines(t *testing.T) {
	c := seededClient()
	// Out-of-range values must not error; they are clamped internally.
	if _, err := c.GetPodLogs(context.Background(), "default", "web-abc", 999999); err != nil {
		t.Fatalf("GetPodLogs (over max): %v", err)
	}
	if _, err := c.GetPodLogs(context.Background(), "default", "web-abc", -5); err != nil {
		t.Fatalf("GetPodLogs (negative): %v", err)
	}
}

func TestNew_LazyUnavailable(t *testing.T) {
	// A bogus kubeconfig path must surface ErrUnavailable on first use,
	// not panic and not succeed. Set a non-empty path so restConfig does
	// not fall back to a workstation's real ~/.kube/config.
	c := New("/nonexistent/kubeconfig/path/that/does/not/exist")
	_, err := c.ListNamespaces(context.Background())
	if err == nil {
		t.Fatal("expected error from bogus kubeconfig, got nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}
