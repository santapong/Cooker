package model

// KubeWorkload represents a Kubernetes workload resource.
type KubeWorkload struct {
	Kind      string `json:"kind"` // Deployment, StatefulSet, DaemonSet
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Replicas  int32  `json:"replicas"`
	Ready     int32  `json:"ready"`
	Image     string `json:"image"`
	Status    string `json:"status"`
}

// KubeNamespace represents a Kubernetes namespace.
type KubeNamespace struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Labels map[string]string `json:"labels"`
}

// KubePod represents a Kubernetes pod.
type KubePod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
	Node      string `json:"node"`
	IP        string `json:"ip"`
	Restarts  int32  `json:"restarts"`
}

// ScaleRequest is the request body for scaling a workload.
type ScaleRequest struct {
	Replicas int32 `json:"replicas" binding:"required,min=0"`
}

// ApplyRequest is the request body for applying K8s manifests.
type ApplyRequest struct {
	Manifest  string `json:"manifest" binding:"required"`
	Namespace string `json:"namespace"`
}
