package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/kube"
	"github.com/santapong/cooker/internal/model"
)

// kubeUnavailable writes the consistent 503 used when the Kubernetes
// client isn't configured (nil Kube) or the configured cluster can't be
// reached (kube.ErrUnavailable). The shape mirrors the other "feature
// gap" responses so the UI can render a clear empty/disabled state.
func kubeUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": "kubernetes cluster not configured or unreachable",
		"hint":  "set COOKER_KUBECONFIG (or run in-cluster) so the server can reach a cluster",
	})
}

// abortKubeErr maps a kube package error to an HTTP response. A nil
// client or kube.ErrUnavailable becomes 503; anything else is a 500
// with the wrapped message. Returns true when it wrote a response.
func abortKubeErr(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, kube.ErrUnavailable) {
		kubeUnavailable(c)
		return true
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	return true
}

// ListNamespaces returns all namespaces in the configured cluster.
func (h *Handler) ListNamespaces(c *gin.Context) {
	if h.Kube == nil {
		kubeUnavailable(c)
		return
	}
	namespaces, err := h.Kube.ListNamespaces(c.Request.Context())
	if abortKubeErr(c, err) {
		return
	}
	c.JSON(http.StatusOK, namespaces)
}

// ListWorkloads returns Deployments, StatefulSets, and DaemonSets,
// optionally filtered by ?namespace (empty lists across all namespaces).
func (h *Handler) ListWorkloads(c *gin.Context) {
	if h.Kube == nil {
		kubeUnavailable(c)
		return
	}
	namespace := c.Query("namespace")
	workloads, err := h.Kube.ListWorkloads(c.Request.Context(), namespace)
	if abortKubeErr(c, err) {
		return
	}
	c.JSON(http.StatusOK, workloads)
}

// GetWorkload fetches a single workload by namespace/kind/name. An
// unknown kind surfaces as 400 (client error) rather than a confusing
// 500/503.
func (h *Handler) GetWorkload(c *gin.Context) {
	if h.Kube == nil {
		kubeUnavailable(c)
		return
	}
	ns := c.Param("ns")
	kind := c.Param("kind")
	name := c.Param("name")
	w, err := h.Kube.GetWorkload(c.Request.Context(), ns, kind, name)
	if err != nil {
		if errors.Is(err, kube.ErrUnavailable) {
			kubeUnavailable(c)
			return
		}
		// Unknown kind is the caller's fault; the kube package returns a
		// plain error (not ErrUnavailable) for it.
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, w)
}

// GetPodLogs returns the tail of a pod's logs. ?tailLines bounds the
// number of lines (the kube package clamps it to [1, 10000]).
func (h *Handler) GetPodLogs(c *gin.Context) {
	if h.Kube == nil {
		kubeUnavailable(c)
		return
	}
	ns := c.Param("ns")
	name := c.Param("name")
	var tail int64
	if v := c.Query("tailLines"); v != "" {
		// A malformed value is ignored (tail stays 0 → the package
		// default); the endpoint doesn't 400 on a cosmetic query param.
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			tail = n
		}
	}
	logs, err := h.Kube.GetPodLogs(c.Request.Context(), ns, name, tail)
	if abortKubeErr(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// --- Write path: not implemented (honest 501). ---
//
// ScaleWorkload, RestartWorkload, ApplyManifest, and DeleteResource are
// not built. The mutating Kubernetes path is deliberately out of scope
// for the read-only list/inspect work (h.Kube is a read-only client) and
// is tracked separately.
//
// Until they are implemented these return 501 with the consistent
// {error,operation,hint} envelope rather than the fake 2xx
// ("scaled" / "rolling restart initiated" / "manifest applied" /
// "resource deleted") they used to. The fake success made the UI report
// a mutation that never reached any cluster
// (docs/audits/2026-05-half-shipped.md HS26-05-05). 501 makes the api
// client throw so the UI surfaces an honest error.
//
// Note: this is distinct from the read-path 503 (kubeUnavailable). The
// read client may be perfectly configured and reachable; the write path
// simply isn't wired yet, so "not implemented" is the accurate status,
// not "cluster unavailable".

// notImplementedKubeWrite writes the 501 used for every unbuilt mutating
// Kubernetes endpoint. Shape mirrors notImplementedDockerHost so the UI
// can branch on `operation`.
func notImplementedKubeWrite(c *gin.Context, op string) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":     "kubernetes write operations are not implemented",
		"operation": op,
		"hint":      "the mutating kubernetes path is not wired yet; use kubectl or a GitOps pipeline for cluster mutations",
	})
}

func (h *Handler) ScaleWorkload(c *gin.Context) {
	// Keep validating the body so a malformed request still 400s ahead of
	// the not-implemented signal (matches the docker write handlers).
	var req model.ScaleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	notImplementedKubeWrite(c, "workload.scale")
}

func (h *Handler) RestartWorkload(c *gin.Context) {
	notImplementedKubeWrite(c, "workload.restart")
}

func (h *Handler) ApplyManifest(c *gin.Context) {
	var req model.ApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	notImplementedKubeWrite(c, "manifest.apply")
}

func (h *Handler) DeleteResource(c *gin.Context) {
	notImplementedKubeWrite(c, "resource.delete")
}
