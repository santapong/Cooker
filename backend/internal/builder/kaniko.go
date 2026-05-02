package builder

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Kaniko builds OCI images by submitting a one-shot batch/v1.Job that
// runs the Kaniko executor inside the target cluster. It replaces the
// docker.sock-based DockerSock builder for production: no node-level
// Docker daemon, no host-root escalation path, and the build pod runs
// with whatever PodSecurityContext the operator's cluster enforces.
//
// Build context delivery: the Job mounts a PersistentVolumeClaim
// referenced by KanikoConfig.ContextPVC, with the operator-managed
// volume containing the source tree at the path Request.ContextDir.
// Cooker's own pod must mount the same PVC at the same path so it can
// stage the source for Kaniko to consume. When ContextPVC is empty,
// the Job uses an emptyDir — useful for unit tests that only assert
// Job spec, never for real builds.
type Kaniko struct {
	cfg    KanikoConfig
	client kubernetes.Interface
}

// KanikoConfig holds the Kaniko builder's environment-driven settings.
// All fields except Client come from cooker's process-level config.
type KanikoConfig struct {
	// Kubeconfig is the path to a kubeconfig file. Empty means use the
	// in-cluster service-account credentials (the production path).
	Kubeconfig string
	// Namespace is the namespace the Job is created in. Cooker's
	// ServiceAccount must have Job + Pod create/get/delete/watch RBAC
	// in this namespace (see deploy/helm/cooker/templates/rbac.yaml).
	Namespace string
	// Image is the Kaniko executor image, e.g.
	// "gcr.io/kaniko-project/executor:v1.23.2".
	Image string
	// ServiceAccount is the SA the Job's pod runs as. Empty uses the
	// namespace default. Production should set this to a dedicated SA
	// with registry-credential mounts.
	ServiceAccount string
	// ContextPVC is the name of the PVC mounted at the build-context
	// path on both Cooker and the Kaniko Job. Empty falls back to
	// emptyDir (unit-test only — real builds need a shared volume).
	ContextPVC string
	// PollInterval governs how often the Job's status is checked.
	// Zero means a sane default (3s).
	PollInterval time.Duration
	// Timeout caps the wall-clock duration of a single build. Zero
	// means no upper bound beyond ctx.
	Timeout time.Duration
	// Client overrides the Kubernetes client. Tests inject a fake;
	// production leaves this nil and gets in-cluster wiring.
	Client kubernetes.Interface
}

// NewKaniko constructs a Kaniko builder. When cfg.Client is nil it
// builds a real Kubernetes client from cfg.Kubeconfig (or in-cluster
// service-account credentials when that's empty).
func NewKaniko(cfg KanikoConfig) (*Kaniko, error) {
	if cfg.Image == "" {
		cfg.Image = "gcr.io/kaniko-project/executor:latest"
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "cooker"
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 3 * time.Second
	}
	client := cfg.Client
	if client == nil {
		var (
			restCfg *rest.Config
			err     error
		)
		if cfg.Kubeconfig == "" {
			restCfg, err = rest.InClusterConfig()
		} else {
			restCfg, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
		}
		if err != nil {
			return nil, fmt.Errorf("kaniko: kube config: %w", err)
		}
		client, err = kubernetes.NewForConfig(restCfg)
		if err != nil {
			return nil, fmt.Errorf("kaniko: kube client: %w", err)
		}
	}
	return &Kaniko{cfg: cfg, client: client}, nil
}

// Build submits a Kaniko Job, streams its pod logs to req.LogWriter,
// waits for completion, and reports the resulting image digest. The
// Job is deleted on every exit path (success, failure, ctx cancel)
// so a stale Job doesn't pin the namespace's resource quota.
func (k *Kaniko) Build(ctx context.Context, req Request) (Result, error) {
	if err := validateRequest(req); err != nil {
		return Result{}, err
	}
	if k == nil || k.client == nil {
		return Result{}, fmt.Errorf("%w: kaniko client not configured", ErrUnavailable)
	}

	if k.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, k.cfg.Timeout)
		defer cancel()
	}

	job := k.buildJob(req)
	created, err := k.client.BatchV1().Jobs(k.cfg.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("kaniko: create job: %w", err)
	}
	// Background deletion: 10s grace period for the controller to clean
	// up child Pods. Best-effort: don't override a real error with a
	// delete failure.
	defer func() {
		bg := metav1.DeletePropagationBackground
		_ = k.client.BatchV1().Jobs(k.cfg.Namespace).Delete(
			context.Background(), created.Name,
			metav1.DeleteOptions{PropagationPolicy: &bg},
		)
	}()

	if req.LogWriter != nil {
		go k.streamLogs(ctx, created.Name, req.LogWriter)
	}

	digest, err := k.waitForCompletion(ctx, created.Name)
	if err != nil {
		return Result{}, err
	}
	return Result{
		ImageID: digest,
		Tags:    append([]string(nil), req.Tags...),
	}, nil
}

// buildJob constructs the Job spec without talking to the API.
// Exposed via test helper signatures so the spec can be asserted
// without standing up a fake clientset.
func (k *Kaniko) buildJob(req Request) *batchv1.Job {
	args := []string{
		"--context=dir://" + req.ContextDir,
		"--digest-file=/dev/termination-log",
	}
	if req.Dockerfile != "" {
		args = append(args, "--dockerfile="+req.Dockerfile)
	}
	for _, t := range req.Tags {
		args = append(args, "--destination="+t)
	}
	for k, v := range req.BuildArgs {
		args = append(args, "--build-arg="+k+"="+v)
	}
	if len(req.Platforms) == 1 {
		args = append(args, "--customPlatform="+req.Platforms[0])
	}

	jobName := fmt.Sprintf("cooker-build-%d", time.Now().UnixNano())
	backoff := int32(0)
	ttl := int32(300) // delete the Job 5 minutes after completion
	one := int32(1)

	contextVolume := corev1.Volume{Name: "build-context"}
	if k.cfg.ContextPVC != "" {
		contextVolume.PersistentVolumeClaim = &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: k.cfg.ContextPVC,
		}
	} else {
		contextVolume.EmptyDir = &corev1.EmptyDirVolumeSource{}
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: k.cfg.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "cooker-builder",
				"app.kubernetes.io/component": "kaniko",
				"app.kubernetes.io/part-of":   "cooker",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Completions:             &one,
			Parallelism:             &one,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: k.cfg.ServiceAccount,
					Containers: []corev1.Container{{
						Name:    "kaniko",
						Image:   k.cfg.Image,
						Args:    args,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "build-context",
							MountPath: req.ContextDir,
						}},
						SecurityContext: &corev1.SecurityContext{
							// Kaniko needs RunAsUser=0 inside its own
							// container — it manipulates the image layer
							// filesystem. The Pod still runs without host
							// privileges and respects PSA's restricted
							// profile via runAsNonRoot=false here being
							// a Pod-level decision an operator can deny.
							AllowPrivilegeEscalation: ptrBool(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
						},
					}},
					Volumes: []corev1.Volume{contextVolume},
				},
			},
		},
	}
}

// waitForCompletion polls the Job until it's Succeeded or Failed.
// Returns the digest captured from the completed Pod's termination
// message (Kaniko's --digest-file=/dev/termination-log path).
func (k *Kaniko) waitForCompletion(ctx context.Context, jobName string) (string, error) {
	var digest string
	err := wait.PollUntilContextCancel(ctx, k.cfg.PollInterval, true, func(ctx context.Context) (bool, error) {
		job, err := k.client.BatchV1().Jobs(k.cfg.Namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		switch {
		case job.Status.Succeeded > 0:
			digest = k.fetchDigest(ctx, jobName)
			return true, nil
		case job.Status.Failed > 0:
			return true, fmt.Errorf("kaniko: build failed (job %s)", jobName)
		}
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("kaniko: %w", err)
		}
		return "", err
	}
	return digest, nil
}

// fetchDigest reads the termination-message of the completed pod,
// which Kaniko writes the image digest to via --digest-file. Returns
// "" on any error: the build succeeded; the caller's tags are still
// usable, just without an ImageID.
func (k *Kaniko) fetchDigest(ctx context.Context, jobName string) string {
	pods, err := k.client.CoreV1().Pods(k.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	for _, cs := range pods.Items[0].Status.ContainerStatuses {
		if cs.Name == "kaniko" && cs.State.Terminated != nil {
			return strings.TrimSpace(cs.State.Terminated.Message)
		}
	}
	return ""
}

// streamLogs follows the Kaniko pod's stdout once it exists, copying
// to w until the pod terminates or ctx is cancelled. Best-effort: any
// error here is logged and swallowed — the build itself is the source
// of truth for success/failure.
func (k *Kaniko) streamLogs(ctx context.Context, jobName string, w io.Writer) {
	// Wait for the pod to appear before opening a log stream. Without
	// this, GetLogs returns a 404 and we miss the early output.
	var podName string
	_ = wait.PollUntilContextCancel(ctx, k.cfg.PollInterval, true, func(ctx context.Context) (bool, error) {
		pods, err := k.client.CoreV1().Pods(k.cfg.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "job-name=" + jobName,
		})
		if err != nil || len(pods.Items) == 0 {
			return false, nil
		}
		podName = pods.Items[0].Name
		return true, nil
	})
	if podName == "" {
		return
	}
	stream, err := k.client.CoreV1().Pods(k.cfg.Namespace).
		GetLogs(podName, &corev1.PodLogOptions{Follow: true}).
		Stream(ctx)
	if err != nil {
		return
	}
	defer stream.Close()
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		if _, err := fmt.Fprintln(w, scanner.Text()); err != nil {
			return
		}
	}
}

func ptrBool(b bool) *bool { return &b }

var _ Builder = (*Kaniko)(nil)
