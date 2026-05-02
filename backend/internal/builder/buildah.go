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

// Buildah is the third in-cluster builder option (alongside Kaniko
// and the docker.sock fallback). It submits a one-shot batch/v1.Job
// running quay.io/buildah/stable, which executes `buildah bud`
// against the build context.
//
// Operators pick Buildah over Kaniko when they need full Dockerfile
// feature parity (RUN --mount=type=cache, --mount=type=secret,
// heredocs, …) — Kaniko silently ignores those directives. The
// trade-off is the PodSecurityAdmission story: rootless Buildah
// needs CAP_SETUID + CAP_SETGID for its user-namespace setup, so
// the build namespace must opt into "baseline" or a custom profile;
// "restricted" drops both caps and breaks the build. See SECURITY.md.
type Buildah struct {
	cfg    BuildahConfig
	client kubernetes.Interface
}

// BuildahConfig mirrors KanikoConfig but for Buildah. The only
// Buildah-specific knob is StorageDriver — overlay (with
// fuse-overlayfs on the nodes) is fast but needs the FS module on
// each node; vfs is slower but works on any kernel.
type BuildahConfig struct {
	Kubeconfig     string
	Namespace      string
	Image          string // default "quay.io/buildah/stable:latest"
	ServiceAccount string
	ContextPVC     string
	StorageDriver  string // "overlay" | "vfs"; default "vfs"
	PollInterval   time.Duration
	Timeout        time.Duration
	Client         kubernetes.Interface
}

// NewBuildah constructs a Buildah builder. ctx-less wiring is
// intentional — config errors should fail fast at process start.
func NewBuildah(cfg BuildahConfig) (*Buildah, error) {
	if cfg.Image == "" {
		cfg.Image = "quay.io/buildah/stable:latest"
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "cooker"
	}
	if cfg.StorageDriver == "" {
		cfg.StorageDriver = "vfs"
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
			return nil, fmt.Errorf("buildah: kube config: %w", err)
		}
		client, err = kubernetes.NewForConfig(restCfg)
		if err != nil {
			return nil, fmt.Errorf("buildah: kube client: %w", err)
		}
	}
	return &Buildah{cfg: cfg, client: client}, nil
}

func (b *Buildah) Build(ctx context.Context, req Request) (Result, error) {
	if err := validateRequest(req); err != nil {
		return Result{}, err
	}
	if b == nil || b.client == nil {
		return Result{}, fmt.Errorf("%w: buildah client not configured", ErrUnavailable)
	}
	if b.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.cfg.Timeout)
		defer cancel()
	}

	job := b.buildJob(req)
	created, err := b.client.BatchV1().Jobs(b.cfg.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("buildah: create job: %w", err)
	}
	defer func() {
		bg := metav1.DeletePropagationBackground
		_ = b.client.BatchV1().Jobs(b.cfg.Namespace).Delete(
			context.Background(), created.Name,
			metav1.DeleteOptions{PropagationPolicy: &bg},
		)
	}()

	if req.LogWriter != nil {
		go b.streamLogs(ctx, created.Name, req.LogWriter)
	}

	digest, err := b.waitForCompletion(ctx, created.Name)
	if err != nil {
		return Result{}, err
	}
	return Result{
		ImageID: digest,
		Tags:    append([]string(nil), req.Tags...),
	}, nil
}

// buildJob assembles the batch/v1.Job spec. The container shells out
// to `buildah bud` and `buildah push` per tag.
func (b *Buildah) buildJob(req Request) *batchv1.Job {
	dockerfile := req.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	// Compose a single shell command so we can write the digest to
	// the termination-log on success.
	parts := []string{
		fmt.Sprintf("buildah bud --storage-driver=%s -f %s -t cooker-build:current %s", b.cfg.StorageDriver, dockerfile, req.ContextDir),
	}
	for k, v := range req.BuildArgs {
		parts[0] += fmt.Sprintf(" --build-arg %s=%s", k, v)
	}
	for _, t := range req.Tags {
		parts = append(parts, fmt.Sprintf("buildah push --storage-driver=%s cooker-build:current docker://%s", b.cfg.StorageDriver, t))
	}
	parts = append(parts, fmt.Sprintf("buildah inspect --storage-driver=%s --format '{{.FromImageDigest}}' cooker-build:current > /dev/termination-log", b.cfg.StorageDriver))
	cmd := strings.Join(parts, " && ")

	jobName := fmt.Sprintf("cooker-buildah-%d", time.Now().UnixNano())
	backoff := int32(0)
	ttl := int32(300)
	one := int32(1)

	contextVolume := corev1.Volume{Name: "build-context"}
	if b.cfg.ContextPVC != "" {
		contextVolume.PersistentVolumeClaim = &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: b.cfg.ContextPVC,
		}
	} else {
		contextVolume.EmptyDir = &corev1.EmptyDirVolumeSource{}
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: b.cfg.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "cooker-builder",
				"app.kubernetes.io/component": "buildah",
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
					ServiceAccountName: b.cfg.ServiceAccount,
					Containers: []corev1.Container{{
						Name:    "buildah",
						Image:   b.cfg.Image,
						Command: []string{"/bin/sh", "-c"},
						Args:    []string{cmd},
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
							AllowPrivilegeEscalation: ptrBool(false),
							Capabilities: &corev1.Capabilities{
								// Rootless Buildah needs SETUID/SETGID
								// for its user-namespace setup.
								Drop: []corev1.Capability{"ALL"},
								Add:  []corev1.Capability{"SETUID", "SETGID"},
							},
						},
					}},
					Volumes: []corev1.Volume{contextVolume},
				},
			},
		},
	}
}

func (b *Buildah) waitForCompletion(ctx context.Context, jobName string) (string, error) {
	var digest string
	err := wait.PollUntilContextCancel(ctx, b.cfg.PollInterval, true, func(ctx context.Context) (bool, error) {
		job, err := b.client.BatchV1().Jobs(b.cfg.Namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		switch {
		case job.Status.Succeeded > 0:
			digest = b.fetchDigest(ctx, jobName)
			return true, nil
		case job.Status.Failed > 0:
			return true, fmt.Errorf("buildah: build failed (job %s)", jobName)
		}
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("buildah: %w", err)
		}
		return "", err
	}
	return digest, nil
}

func (b *Buildah) fetchDigest(ctx context.Context, jobName string) string {
	pods, err := b.client.CoreV1().Pods(b.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	for _, cs := range pods.Items[0].Status.ContainerStatuses {
		if cs.Name == "buildah" && cs.State.Terminated != nil {
			return strings.TrimSpace(cs.State.Terminated.Message)
		}
	}
	return ""
}

func (b *Buildah) streamLogs(ctx context.Context, jobName string, w io.Writer) {
	var podName string
	_ = wait.PollUntilContextCancel(ctx, b.cfg.PollInterval, true, func(ctx context.Context) (bool, error) {
		pods, err := b.client.CoreV1().Pods(b.cfg.Namespace).List(ctx, metav1.ListOptions{
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
	stream, err := b.client.CoreV1().Pods(b.cfg.Namespace).
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

var _ Builder = (*Buildah)(nil)
