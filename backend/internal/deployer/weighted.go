package deployer

import (
	"fmt"
	"strings"
)

// defaultCanaryPoolReplicas is the total replica pool a weight is split
// over when WeightedRequest.Replicas is unset. Four lets a 25%-step
// canary land on whole pods cleanly (1/4 = 25%).
const defaultCanaryPoolReplicas = 4

// splitReplicas divides a replica pool between canary and stable by
// weight. The canary count is rounded to the nearest whole pod, then
// clamped so an in-progress canary (0 < weight < 100) always keeps at
// least one pod on each side — otherwise "10% of 4 pods" would round to
// zero canary pods and ship nothing. weight<=0 yields an all-stable
// split (abort); weight>=100 yields an all-canary split (promote).
func splitReplicas(weight, pool int) (canary, stable int) {
	if pool <= 0 {
		pool = defaultCanaryPoolReplicas
	}
	switch {
	case weight <= 0:
		return 0, pool
	case weight >= 100:
		return pool, 0
	}
	canary = (weight*pool + 50) / 100 // round to nearest
	if canary < 1 {
		canary = 1
	}
	if canary > pool-1 {
		canary = pool - 1 // leave at least one stable pod during rollout
	}
	return canary, pool - canary
}

// canaryManifest renders the stable + canary Deployment pair (and a
// shared Service) for a replica-weighted canary. Both Deployments share
// the app label so one Service load-balances across them; the canary
// Deployment carries an extra track=canary label for observability.
// When canaryReplicas is 0 the canary Deployment is still emitted at 0
// replicas so a subsequent abort apply scales it down rather than
// orphaning it. A promote (stableReplicas 0) keeps the stable Deployment
// at 0 for the same reason.
func canaryManifest(name, stableImage, canaryImage string, canaryReplicas, stableReplicas int) string {
	app := sanitizeName(name)
	stable := app
	canary := app + "-canary"
	var b strings.Builder
	writeDeployment(&b, stable, app, "stable", stableImage, stableReplicas)
	b.WriteString("---\n")
	writeDeployment(&b, canary, app, "canary", canaryImage, canaryReplicas)
	b.WriteString("---\n")
	// A single Service selecting only the app label so it fans traffic
	// across both tracks in proportion to their ready pod counts.
	fmt.Fprintf(&b, `apiVersion: v1
kind: Service
metadata:
  name: %[1]s
spec:
  selector: {app: %[1]s}
  ports: [{port: 80, targetPort: 80}]
`, app)
	return b.String()
}

// writeDeployment appends one Deployment doc to b. selector is the app
// label shared by both tracks (so the Service load-balances across
// them); track distinguishes the pods for humans / kubectl. The pod
// template carries both labels.
func writeDeployment(b *strings.Builder, depName, app, track, image string, replicas int) {
	fmt.Fprintf(b, `apiVersion: apps/v1
kind: Deployment
metadata:
  name: %[1]s
  labels: {app: %[2]s, track: %[3]s}
spec:
  replicas: %[5]d
  selector:
    matchLabels: {app: %[2]s, track: %[3]s}
  template:
    metadata:
      labels: {app: %[2]s, track: %[3]s}
    spec:
      containers:
        - name: %[2]s
          image: %[4]s
          ports: [{containerPort: 80}]
`, depName, app, track, image, replicas)
}

// sanitizeName returns a DNS-1123-safe slug. Mirrors service.sanitize
// but kept local so the deployer package stays free of a service import.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

// weightedManifestFor builds the canary manifest and replica split for a
// request. Shared by the Kubectl and ClientGo DeployWeighted impls so
// the rounding and YAML stay identical across backends.
func weightedManifestFor(req WeightedRequest) (manifest string, canaryReplicas, stableReplicas int) {
	canaryReplicas, stableReplicas = splitReplicas(req.Weight, req.Replicas)
	manifest = canaryManifest(req.Name, req.StableImage, req.CanaryImage, canaryReplicas, stableReplicas)
	return manifest, canaryReplicas, stableReplicas
}
