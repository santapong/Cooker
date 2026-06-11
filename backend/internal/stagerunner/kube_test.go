package stagerunner

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func newKubeForTest(t *testing.T, objects ...runtime.Object) *Kube {
	t.Helper()
	client := fake.NewSimpleClientset(objects...)
	k, err := NewKube(KubeConfig{
		Namespace:    "test-ns",
		PollInterval: 10 * time.Millisecond,
		Client:       client,
	})
	if err != nil {
		t.Fatalf("NewKube: %v", err)
	}
	return k
}

func TestKube_Validation(t *testing.T) {
	k := newKubeForTest(t)
	if _, err := k.Run(context.Background(), Request{}); err == nil {
		t.Fatal("expected validation error for empty Request (no image)")
	}
}

func TestKube_BuildJob_Spec(t *testing.T) {
	k := newKubeForTest(t)
	job := k.buildJob(Request{
		Image:   "node:20-alpine",
		Script:  "npm test",
		Env:     map[string]string{"CI": "1"},
		WorkDir: "/app",
	})
	if job.Namespace != "test-ns" {
		t.Errorf("namespace = %q, want test-ns", job.Namespace)
	}
	if !strings.HasPrefix(job.Name, "cooker-stage-") {
		t.Errorf("name = %q, want cooker-stage- prefix", job.Name)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if c.Image != "node:20-alpine" {
		t.Errorf("image = %q, want node:20-alpine", c.Image)
	}
	if strings.Join(c.Command, " ") != "sh -c npm test" {
		t.Errorf("command = %v, want [sh -c npm test]", c.Command)
	}
	if c.WorkingDir != "/app" {
		t.Errorf("workdir = %q, want /app", c.WorkingDir)
	}
	if len(c.Env) != 1 || c.Env[0].Name != "CI" || c.Env[0].Value != "1" {
		t.Errorf("env = %v, want [CI=1]", c.Env)
	}
	if c.SecurityContext == nil || c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation {
		t.Error("container should drop privilege escalation")
	}
}

func TestKube_Run_SuccessExitZero(t *testing.T) {
	k := newKubeForTest(t)
	fakeClient := k.client.(*fake.Clientset)
	var createdJob string
	var deleteCalls int32
	fakeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		j := action.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		createdJob = j.Name
		j.Status.Succeeded = 1
		return false, nil, nil
	})
	fakeClient.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&deleteCalls, 1)
		return false, nil, nil
	})
	fakeClient.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.PodList{Items: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "stage-pod-1", Namespace: "test-ns", Labels: map[string]string{"job-name": createdJob}},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "stage",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
			}}},
		}}}, nil
	})

	res, err := k.Run(context.Background(), Request{Image: "busybox", Command: []string{"true"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", res.ExitCode)
	}
	if atomic.LoadInt32(&deleteCalls) == 0 {
		t.Error("expected the Job to be deleted on success")
	}
}

func TestKube_Run_FailedJobReportsExitCode(t *testing.T) {
	k := newKubeForTest(t)
	fakeClient := k.client.(*fake.Clientset)
	var createdJob string
	fakeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		j := action.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		createdJob = j.Name
		j.Status.Failed = 1
		return false, nil, nil
	})
	fakeClient.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.PodList{Items: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "stage-pod-1", Namespace: "test-ns", Labels: map[string]string{"job-name": createdJob}},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "stage",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 7}},
			}}},
		}}}, nil
	})

	res, err := k.Run(context.Background(), Request{Image: "busybox", Command: []string{"sh", "-c", "exit 7"}})
	if err != nil {
		t.Fatalf("a failed container is a stage failure, not a runtime error; got %v", err)
	}
	if res.ExitCode != 7 {
		t.Errorf("exit = %d, want 7", res.ExitCode)
	}
}

func TestKube_Run_CtxCancel(t *testing.T) {
	k := newKubeForTest(t)
	fakeClient := k.client.(*fake.Clientset)
	// Job never reaches terminal: Get always returns Active.
	fakeClient.PrependReactor("get", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "j1", Namespace: "test-ns"},
			Status:     batchv1.JobStatus{Active: 1},
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := k.Run(ctx, Request{Image: "busybox", Command: []string{"sleep", "60"}})
	if err == nil {
		t.Fatal("expected ctx cancellation to surface as an error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error chain should include context.DeadlineExceeded; got %v", err)
	}
}

func TestKube_NilClientUnavailable(t *testing.T) {
	k := &Kube{cfg: KubeConfig{Namespace: "test-ns"}}
	_, err := k.Run(context.Background(), Request{Image: "x"})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable chain", err)
	}
}
