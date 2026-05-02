package builder

import (
	"context"
	"errors"
	"sort"
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

func newKanikoForTest(t *testing.T, ctxPVC string, objects ...runtime.Object) *Kaniko {
	t.Helper()
	client := fake.NewSimpleClientset(objects...)
	k, err := NewKaniko(KanikoConfig{
		Namespace:    "test-ns",
		Image:        "kaniko:test",
		ContextPVC:   ctxPVC,
		PollInterval: 10 * time.Millisecond,
		Client:       client,
	})
	if err != nil {
		t.Fatalf("NewKaniko: %v", err)
	}
	return k
}

func TestKaniko_Validation(t *testing.T) {
	k := newKanikoForTest(t, "")

	if _, err := k.Build(context.Background(), Request{}); err == nil {
		t.Fatal("expected validation error for empty Request")
	}
	if _, err := k.Build(context.Background(), Request{ContextDir: "/x"}); err == nil {
		t.Fatal("expected validation error for missing Tags")
	}
}

func TestKaniko_BuildJob_Args(t *testing.T) {
	k := newKanikoForTest(t, "src-pvc")

	req := Request{
		ContextDir: "/workspace/src",
		Dockerfile: "build/Dockerfile",
		Tags:       []string{"reg.example.com/app:v1", "reg.example.com/app:latest"},
		BuildArgs:  map[string]string{"GIT_SHA": "abc123"},
		Platforms:  []string{"linux/amd64"},
	}
	job := k.buildJob(req)

	if got, want := job.Namespace, "test-ns"; got != want {
		t.Errorf("Namespace = %q, want %q", got, want)
	}
	if !strings.HasPrefix(job.Name, "cooker-build-") {
		t.Errorf("Name should be prefixed cooker-build-; got %q", job.Name)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if c.Image != "kaniko:test" {
		t.Errorf("container image = %q, want kaniko:test", c.Image)
	}

	args := c.Args
	wantSubs := []string{
		"--context=dir:///workspace/src",
		"--dockerfile=build/Dockerfile",
		"--destination=reg.example.com/app:v1",
		"--destination=reg.example.com/app:latest",
		"--build-arg=GIT_SHA=abc123",
		"--customPlatform=linux/amd64",
		"--digest-file=/dev/termination-log",
	}
	sort.Strings(args)
	for _, want := range wantSubs {
		found := false
		for _, a := range args {
			if a == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("args missing %q\nfull args: %v", want, args)
		}
	}

	// PVC volume must reference the configured ClaimName, not emptyDir.
	if len(job.Spec.Template.Spec.Volumes) != 1 {
		t.Fatalf("want 1 volume, got %d", len(job.Spec.Template.Spec.Volumes))
	}
	v := job.Spec.Template.Spec.Volumes[0]
	if v.PersistentVolumeClaim == nil || v.PersistentVolumeClaim.ClaimName != "src-pvc" {
		t.Errorf("expected PVC volume claim 'src-pvc', got %+v", v)
	}

	// Volume mount path must equal ContextDir so Kaniko's --context
	// dir:// resolves on the same path the operator staged the source at.
	if c.VolumeMounts[0].MountPath != req.ContextDir {
		t.Errorf("mount path = %q, want %q", c.VolumeMounts[0].MountPath, req.ContextDir)
	}

	// Backoff must be 0 — a build that fails should fail fast, not retry.
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Errorf("BackoffLimit = %v, want 0", job.Spec.BackoffLimit)
	}
}

func TestKaniko_BuildJob_EmptyDirFallback(t *testing.T) {
	k := newKanikoForTest(t, "")
	job := k.buildJob(Request{ContextDir: "/x", Tags: []string{"t"}})
	v := job.Spec.Template.Spec.Volumes[0]
	if v.EmptyDir == nil {
		t.Errorf("expected emptyDir fallback when ContextPVC is unset; got %+v", v)
	}
	if v.PersistentVolumeClaim != nil {
		t.Errorf("PVC must not be set when ContextPVC is empty; got %+v", v.PersistentVolumeClaim)
	}
}

func TestKaniko_Build_SucceedsAndReturnsDigest(t *testing.T) {
	k := newKanikoForTest(t, "src-pvc")
	const wantDigest = "sha256:deadbeef"

	// Track create/delete calls so we can assert cleanup happens.
	var createdJob string
	var deleteCalls int32
	fakeClient := k.client.(*fake.Clientset)
	fakeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		j := action.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		createdJob = j.Name
		// Mark Succeeded so waitForCompletion returns on the first poll.
		j.Status.Succeeded = 1
		return false, nil, nil
	})
	fakeClient.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&deleteCalls, 1)
		return false, nil, nil
	})
	// Once the Job appears with Succeeded>0, Build calls fetchDigest,
	// which lists pods and reads the termination message.
	fakeClient.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &corev1.PodList{Items: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kaniko-pod-1",
				Namespace: "test-ns",
				Labels:    map[string]string{"job-name": createdJob},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "kaniko",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{Message: wantDigest},
					},
				}},
			},
		}}}, nil
	})

	res, err := k.Build(context.Background(), Request{
		ContextDir: "/src",
		Tags:       []string{"reg/app:v1"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.ImageID != wantDigest {
		t.Errorf("ImageID = %q, want %q", res.ImageID, wantDigest)
	}
	if len(res.Tags) != 1 || res.Tags[0] != "reg/app:v1" {
		t.Errorf("Tags = %v, want [reg/app:v1]", res.Tags)
	}
	if atomic.LoadInt32(&deleteCalls) == 0 {
		t.Error("expected the Job to be deleted on success (resource cleanup)")
	}
}

func TestKaniko_Build_FailedJobReturnsError(t *testing.T) {
	k := newKanikoForTest(t, "src-pvc")
	fakeClient := k.client.(*fake.Clientset)
	fakeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		j := action.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		j.Status.Failed = 1
		return false, nil, nil
	})

	_, err := k.Build(context.Background(), Request{
		ContextDir: "/src",
		Tags:       []string{"reg/app:v1"},
	})
	if err == nil {
		t.Fatal("expected error for failed Job")
	}
	if !strings.Contains(err.Error(), "kaniko: build failed") {
		t.Errorf("error = %v, want substring 'kaniko: build failed'", err)
	}
}

func TestKaniko_Build_CtxCancelDeletesJob(t *testing.T) {
	k := newKanikoForTest(t, "src-pvc")
	fakeClient := k.client.(*fake.Clientset)

	// Create succeeds; subsequent Get returns Pending so the poll loop
	// keeps spinning until ctx is cancelled.
	fakeClient.PrependReactor("get", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "j1", Namespace: "test-ns"},
			Status:     batchv1.JobStatus{Active: 1},
		}, nil
	})
	var deleteCalls int32
	fakeClient.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		atomic.AddInt32(&deleteCalls, 1)
		return false, nil, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := k.Build(ctx, Request{
		ContextDir: "/src",
		Tags:       []string{"reg/app:v1"},
	})
	if err == nil {
		t.Fatal("expected ctx cancellation to surface as an error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error chain should include context.DeadlineExceeded; got %v", err)
	}
	if atomic.LoadInt32(&deleteCalls) == 0 {
		t.Error("expected the Job to be deleted on ctx cancel")
	}
}

func TestKaniko_NilClientReturnsUnavailable(t *testing.T) {
	// Bypass NewKaniko: simulate a struct with no client wired.
	k := &Kaniko{cfg: KanikoConfig{Namespace: "test-ns", Image: "x"}}
	_, err := k.Build(context.Background(), Request{ContextDir: "/x", Tags: []string{"t"}})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("error = %v, want ErrUnavailable chain", err)
	}
}
