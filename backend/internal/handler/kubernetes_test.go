package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/santapong/cooker/internal/kube"
	"github.com/santapong/cooker/internal/model"
)

func kubeInt32Ptr(i int32) *int32 { return &i }

// fakeKubeClient returns a kube.Client backed by a fake clientset
// holding one Deployment and one Namespace, for the handler-level tests.
func fakeKubeClient() *kube.Client {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: kubeInt32Ptr(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "web", Image: "nginx:1.25"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 2},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
	return kube.NewWithClientset(fake.NewSimpleClientset(dep, ns))
}

func TestListNamespaces_NilKube503(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/namespaces", h.ListNamespaces)
	})
	_ = h // h.Kube left nil on purpose
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/namespaces", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListWorkloads_NilKube503(t *testing.T) {
	r, _ := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/workloads", h.ListWorkloads)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/workloads", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetPodLogs_NilKube503(t *testing.T) {
	r, _ := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/pods/:ns/:name/logs", h.GetPodLogs)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pods/default/web-abc/logs", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListNamespaces_FakeOK(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/namespaces", h.ListNamespaces)
	})
	h.Kube = fakeKubeClient()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/namespaces", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got []model.KubeNamespace
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	if len(got) != 1 || got[0].Name != "default" || got[0].Status != "Active" {
		t.Errorf("unexpected namespaces: %+v", got)
	}
}

func TestListWorkloads_FakeOK(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/workloads", h.ListWorkloads)
	})
	h.Kube = fakeKubeClient()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/workloads?namespace=default", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got []model.KubeWorkload
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	if len(got) != 1 || got[0].Kind != "Deployment" || got[0].Name != "web" || got[0].Image != "nginx:1.25" {
		t.Errorf("unexpected workloads: %+v", got)
	}
}

func TestGetWorkload_FakeOK(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/workloads/:ns/:kind/:name", h.GetWorkload)
	})
	h.Kube = fakeKubeClient()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/workloads/default/deployment/web", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got model.KubeWorkload
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	if got.Kind != "Deployment" || got.Name != "web" {
		t.Errorf("unexpected workload: %+v", got)
	}
}

func TestGetWorkload_UnknownKind400(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/workloads/:ns/:kind/:name", h.GetWorkload)
	})
	h.Kube = fakeKubeClient()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/workloads/default/cronjob/web", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown kind, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetPodLogs_FakeOK(t *testing.T) {
	r, h := newTestRouter(func(r *gin.Engine, h *Handler) {
		r.GET("/pods/:ns/:name/logs", h.GetPodLogs)
	})
	h.Kube = fakeKubeClient()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pods/default/web-abc/logs?tailLines=50", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	// The fake clientset returns a canned "fake logs" body.
	if got.Logs == "" {
		t.Errorf("expected non-empty logs, got %q", got.Logs)
	}
}
