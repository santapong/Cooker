package deployer

import (
	"strings"
	"testing"
)

func TestSplitReplicas(t *testing.T) {
	tests := []struct {
		name             string
		weight, pool     int
		wantCan, wantSta int
	}{
		{name: "abort all stable", weight: 0, pool: 4, wantCan: 0, wantSta: 4},
		{name: "negative weight clamps to abort", weight: -5, pool: 4, wantCan: 0, wantSta: 4},
		{name: "promote all canary", weight: 100, pool: 4, wantCan: 4, wantSta: 0},
		{name: "over 100 clamps to promote", weight: 150, pool: 4, wantCan: 4, wantSta: 0},
		{name: "25% of 4 is one pod", weight: 25, pool: 4, wantCan: 1, wantSta: 3},
		{name: "50% of 4 is two pods", weight: 50, pool: 4, wantCan: 2, wantSta: 2},
		{name: "10% rounds up to one canary pod", weight: 10, pool: 4, wantCan: 1, wantSta: 3},
		{name: "90% leaves one stable pod", weight: 90, pool: 4, wantCan: 3, wantSta: 1},
		{name: "default pool when unset", weight: 50, pool: 0, wantCan: 2, wantSta: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			can, sta := splitReplicas(tc.weight, tc.pool)
			if can != tc.wantCan || sta != tc.wantSta {
				t.Errorf("splitReplicas(%d,%d) = (%d,%d), want (%d,%d)",
					tc.weight, tc.pool, can, sta, tc.wantCan, tc.wantSta)
			}
		})
	}
}

func TestSplitReplicas_InProgressAlwaysKeepsBothSides(t *testing.T) {
	// For any 0<weight<100 over a 2-pod pool, both sides keep a pod so a
	// rollout never silently ships zero of either version.
	for w := 1; w < 100; w++ {
		can, sta := splitReplicas(w, 2)
		if can < 1 || sta < 1 {
			t.Fatalf("weight %d over pool 2 produced (%d,%d); both must be >=1", w, can, sta)
		}
	}
}

func TestCanaryManifest_RendersBothTracksAndService(t *testing.T) {
	m := canaryManifest("Shop App", "reg/shop:v1", "reg/shop:v2", 1, 3)
	for _, want := range []string{
		"name: shop-app\n",        // stable Deployment + Service
		"name: shop-app-canary\n", // canary Deployment
		"image: reg/shop:v1",      // stable image
		"image: reg/shop:v2",      // canary image
		"track: canary",           // canary track label
		"track: stable",           // stable track label
		"kind: Service",           // shared Service
		"replicas: 1",             // canary replicas
		"replicas: 3",             // stable replicas
	} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest missing %q\n---\n%s", want, m)
		}
	}
	// Exactly two Deployments and one Service.
	if got := strings.Count(m, "kind: Deployment"); got != 2 {
		t.Errorf("want 2 Deployments, got %d", got)
	}
	if got := strings.Count(m, "kind: Service"); got != 1 {
		t.Errorf("want 1 Service, got %d", got)
	}
}

func TestWeightedDeployerInterface(t *testing.T) {
	// The K8s deployers advertise the weighted capability...
	if _, ok := any(NewClientGo("")).(WeightedDeployer); !ok {
		t.Error("ClientGo should implement WeightedDeployer")
	}
	if _, ok := any(NewKubectl()).(WeightedDeployer); !ok {
		t.Error("Kubectl should implement WeightedDeployer")
	}
	// ...and Noop deliberately does NOT (canary is unsupported there, so
	// the service surfaces ErrCanaryUnsupported).
	if _, ok := any(Noop{}).(WeightedDeployer); ok {
		t.Error("Noop must not implement WeightedDeployer")
	}
}
