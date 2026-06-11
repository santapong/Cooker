package stagerunner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Kube runs a stage command by submitting a one-shot batch/v1.Job to the
// configured cluster — the same mechanism the Kaniko/Buildah builders use,
// so a production install that already runs builds in-cluster runs
// Test/Custom stages with no extra moving parts. The Job's pod runs the
// user command under whatever PodSecurityContext the cluster enforces;
// there is no node-level daemon and no host-root path.
//
// The Job is deleted on every exit path (success, non-zero exit, ctx
// cancel) so a stale Job doesn't pin the namespace's quota.
type Kube struct {
	cfg    KubeConfig
	client kubernetes.Interface
}

// KubeConfig holds the Kube runner's environment-driven settings. The
// shape deliberately mirrors builder.KanikoConfig so server wiring reads
// the same K8s config block.
type KubeConfig struct {
	// Kubeconfig is the path to a kubeconfig file. Empty means in-cluster
	// service-account credentials (the production path).
	Kubeconfig string
	// Namespace the Job is created in. Cooker's ServiceAccount needs Job +
	// Pod create/get/delete/watch RBAC here (same grant the builders use).
	Namespace string
	// ServiceAccount the Job's pod runs as. Empty uses the namespace
	// default.
	ServiceAccount string
	// PollInterval governs status/log polling. Zero means 3s.
	PollInterval time.Duration
	// Timeout caps a single stage's wall-clock duration. Zero means no
	// bound beyond ctx (the executor already applies the per-stage
	// timeout, so leaving this zero is fine).
	Timeout time.Duration
	// Client overrides the Kubernetes client. Tests inject a fake;
	// production leaves this nil and gets in-cluster wiring.
	Client kubernetes.Interface
}

// NewKube constructs a Kube runner. When cfg.Client is nil it builds a
// real client from cfg.Kubeconfig (or in-cluster credentials).
func NewKube(cfg KubeConfig) (*Kube, error) {
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
			return nil, fmt.Errorf("stagerunner: kube config: %w", err)
		}
		client, err = kubernetes.NewForConfig(restCfg)
		if err != nil {
			return nil, fmt.Errorf("stagerunner: kube client: %w", err)
		}
	}
	return &Kube{cfg: cfg, client: client}, nil
}

// Run submits the stage Job, streams its pod logs to req.LogWriter, waits
// for completion, and reports the container's exit code.
func (k *Kube) Run(ctx context.Context, req Request) (Result, error) {
	if err := validateRequest(req); err != nil {
		return Result{}, err
	}
	if k == nil || k.client == nil {
		return Result{}, fmt.Errorf("%w: kube client not configured", ErrUnavailable)
	}
	if k.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, k.cfg.Timeout)
		defer cancel()
	}

	job := k.buildJob(req)
	created, err := k.client.BatchV1().Jobs(k.cfg.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("%w: create job: %v", ErrUnavailable, err)
	}
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

	return k.waitForCompletion(ctx, created.Name)
}

// buildJob constructs the Job spec without talking to the API.
func (k *Kube) buildJob(req Request) *batchv1.Job {
	jobName := fmt.Sprintf("cooker-stage-%d", time.Now().UnixNano())
	backoff := int32(0)
	ttl := int32(300) // delete the Job 5 minutes after completion
	one := int32(1)

	env := make([]corev1.EnvVar, 0, len(req.Env))
	for _, key := range sortedKeys(req.Env) {
		env = append(env, corev1.EnvVar{Name: key, Value: req.Env[key]})
	}

	container := corev1.Container{
		Name:       "stage",
		Image:      req.Image,
		Env:        env,
		WorkingDir: req.WorkDir,
		// shellCommand wraps a Script in `sh -c`; an explicit Command is
		// used verbatim. Empty means "use the image's default", which is a
		// valid Test stage (assert the image starts).
		Command: shellCommand(req),
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptrBool(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: k.cfg.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "cooker-stage",
				"app.kubernetes.io/component": "stagerunner",
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
					Containers:         []corev1.Container{container},
				},
			},
		},
	}
}

// waitForCompletion polls the Job until it Succeeds or Fails, then reads
// the pod's container exit code. A succeeded Job is exit 0; a failed Job's
// exit code is read from the terminated container status so the caller can
// distinguish "the script returned 7" from "the runtime broke".
func (k *Kube) waitForCompletion(ctx context.Context, jobName string) (Result, error) {
	var done bool
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
			done = true
			return true, nil
		case job.Status.Failed > 0:
			done = true
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, fmt.Errorf("stagerunner: %w", err)
		}
		return Result{}, fmt.Errorf("%w: watch job: %v", ErrUnavailable, err)
	}
	if !done {
		return Result{}, fmt.Errorf("%w: job %s never reached a terminal state", ErrUnavailable, jobName)
	}
	return Result{ExitCode: k.fetchExitCode(ctx, jobName)}, nil
}

// fetchExitCode reads the terminated container's exit code from the Job's
// pod. Returns 0 when no terminated status is found (a Succeeded Job),
// which is the success contract.
func (k *Kube) fetchExitCode(ctx context.Context, jobName string) int {
	pods, err := k.client.CoreV1().Pods(k.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil || len(pods.Items) == 0 {
		return 0
	}
	for _, cs := range pods.Items[0].Status.ContainerStatuses {
		if cs.Name == "stage" && cs.State.Terminated != nil {
			return int(cs.State.Terminated.ExitCode)
		}
	}
	return 0
}

// ansiCSIRe matches the common CSI escape sequences build/test tools emit.
// Strip them before they hit the log/WebSocket sinks so a malicious script
// can't inject terminal-control codes that confuse operators reading logs
// (same rationale as the Kaniko builder's stripANSI).
var ansiCSIRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][AB012]`)

func stripANSI(b []byte) []byte { return ansiCSIRe.ReplaceAll(b, nil) }

// streamLogs follows the stage pod's stdout once it exists, copying to w
// until the pod terminates or ctx is cancelled. Best-effort: any error
// here is swallowed — the Job status is the source of truth for the exit.
func (k *Kube) streamLogs(ctx context.Context, jobName string, w io.Writer) {
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
	// Allow lines up to 1 MiB (verbose test output beyond bufio's 64 KiB
	// default would otherwise silently end the stream).
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := stripANSI(scanner.Bytes())
		if _, err := w.Write(append(line, '\n')); err != nil {
			return
		}
	}
}

func ptrBool(b bool) *bool { return &b }

var _ Runner = (*Kube)(nil)
