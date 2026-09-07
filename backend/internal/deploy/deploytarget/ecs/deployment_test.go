package ecs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/santapong/cooker/internal/deploy/deploytarget"
	"github.com/santapong/cooker/internal/model"
)

func TestFargateSizeReviewMatchesSupportedAllocations(t *testing.T) {
	for _, tc := range []struct {
		name                string
		cpu, memory         int64
		wantCPU, wantMemory int32
		bad                 bool
	}{
		{"default", 0, 0, 256, 512, false}, {"768MiB", 0, 768, 256, 1024, false}, {"1536MiB", 0, 1536, 256, 2048, false},
		{"fractionalCPU", 750000000, 768, 1024, 2048, false}, {"8CPU", 8e9, 17000, 8192, 20480, false},
		{"tooLarge", 17e9, 1024, 0, 0, true}, {"negative", -1, 0, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cpu, memory, err := TaskSize(&model.ResourceLimits{NanoCPUs: tc.cpu, MemoryBytes: tc.memory * 1024 * 1024})
			if (err != nil) != tc.bad || cpu != tc.wantCPU || memory != tc.wantMemory {
				t.Fatalf("got %d/%d %v", cpu, memory, err)
			}
		})
	}
}

func TestECSDeployUsesRequestedInputsAndVerifiesReadyRevision(t *testing.T) {
	var task map[string]any
	var calls []string
	rollout := "IN_PROGRESS"
	image := "registry.example.com/shop-api:new"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		op := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "AmazonEC2ContainerServiceV20141113.")
		calls = append(calls, op)
		switch op {
		case "RegisterTaskDefinition":
			if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"taskDefinition":{"taskDefinitionArn":"arn:aws:ecs:region:123:task-definition/shop-api:2"}}`)
		case "UpdateService":
			fmt.Fprint(w, `{"service":{"serviceName":"shop-api"}}`)
		case "DescribeServices":
			fmt.Fprintf(w, `{"services":[{"serviceName":"shop-api","desiredCount":1,"runningCount":1,"pendingCount":0,"taskDefinition":"arn:aws:ecs:region:123:task-definition/shop-api:2","deployments":[{"rolloutState":%q}]}]}`, rollout)
		case "DescribeTaskDefinition":
			fmt.Fprintf(w, `{"taskDefinition":{"containerDefinitions":[{"image":%q}]}}`, image)
		default:
			t.Errorf("unexpected cloud operation %s", op)
			w.WriteHeader(400)
		}
	}))
	defer s.Close()
	target := New("region", "cluster")
	target.ExecutionRole = "execution-role"
	target.Subnets = []string{"subnet"}
	target.SecurityGroups = []string{"security-group"}
	target.cachedCli = awsecs.New(awsecs.Options{Region: "region", Credentials: aws.AnonymousCredentials{}, BaseEndpoint: aws.String(s.URL)})
	target.clientOnce.Do(func() {})
	spec := deploytarget.Spec{AppID: "shop-api", Image: image, Env: map[string]string{"DATABASE_URL": "fake-cloud-sql-url"}, Command: []string{"serve", "--port", "8080"}, Ports: []int{8080}, Resources: &model.ResourceLimits{NanoCPUs: 750000000, MemoryBytes: 768 * 1024 * 1024}, HealthCheck: &model.ContainerHealthCheck{Command: []string{"CMD", "/health"}, Interval: 10, Timeout: 5, Retries: 3}}
	if err := target.Deploy(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if task["cpu"] != "1024" || task["memory"] != "2048" {
		t.Fatalf("wrong Fargate sizing: %+v", task)
	}
	container := task["containerDefinitions"].([]any)[0].(map[string]any)
	if container["image"] != image || len(container["command"].([]any)) != 3 || container["healthCheck"] == nil {
		t.Fatal("runtime fields lost")
	}
	if strings.Contains(strings.Join(calls, ","), "CreateService") {
		t.Fatal("existing service incorrectly recreated")
	}
	st, err := target.Status(context.Background(), "shop-api")
	if err != nil || st.Healthy {
		t.Fatalf("in-progress rollout reported ready: %+v %v", st, err)
	}
	rollout = "COMPLETED"
	st, err = target.Status(context.Background(), "shop-api")
	if err != nil || !st.Healthy {
		t.Fatalf("completed rollout not ready: %+v %v", st, err)
	}
	if err := target.VerifyImage(context.Background(), "shop-api", image); err != nil {
		t.Fatal(err)
	}
	if err := target.VerifyImage(context.Background(), "shop-api", "other-image"); err == nil {
		t.Fatal("wrong image accepted as successful")
	}
	rollout = "FAILED"
	if _, err := target.Status(context.Background(), "shop-api"); err == nil {
		t.Fatal("failed rollout accepted")
	}
}
