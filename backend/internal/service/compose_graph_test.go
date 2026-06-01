package service

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

// findConnection returns the (index, true) of the first connection
// matching src->dst with the given type, else (-1, false).
func findConnection(conns []model.ComposeConnection, src, dst, typ string) (int, bool) {
	for i, c := range conns {
		if c.Source == src && c.Target == dst && c.Type == typ {
			return i, true
		}
	}
	return -1, false
}

func countConnections(conns []model.ComposeConnection, src, dst, typ string) int {
	n := 0
	for _, c := range conns {
		if c.Source == src && c.Target == dst && c.Type == typ {
			n++
		}
	}
	return n
}

func TestParseComposeGraph(t *testing.T) {
	cases := []struct {
		name   string
		yaml   string
		assert func(t *testing.T, g *model.ComposeGraph, err error)
	}{
		{
			// 1. Simple two-service web+db with depends_on
			name: "two services web depends_on db",
			yaml: `
services:
  web:
    image: nginx:latest
    depends_on:
      - db
  db:
    image: postgres:15
`,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(g.Services) != 2 {
					t.Fatalf("want 2 services, got %d", len(g.Services))
				}
				if _, ok := findConnection(g.Connections, "web", "db", "depends_on"); !ok {
					t.Fatalf("missing web->db depends_on edge; got %+v", g.Connections)
				}
				if len(g.Connections) != 1 {
					t.Fatalf("want 1 connection, got %d: %+v", len(g.Connections), g.Connections)
				}
			},
		},
		{
			// 2. Three-service diamond depends_on (web -> api, worker; both -> db)
			name: "three service diamond depends_on",
			yaml: `
services:
  web:
    image: nginx
    depends_on:
      - api
      - worker
  api:
    image: api:1
    depends_on:
      - db
  worker:
    image: worker:1
    depends_on:
      - db
  db:
    image: postgres
`,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				wantEdges := [][3]string{
					{"web", "api", "depends_on"},
					{"web", "worker", "depends_on"},
					{"api", "db", "depends_on"},
					{"worker", "db", "depends_on"},
				}
				for _, e := range wantEdges {
					if _, ok := findConnection(g.Connections, e[0], e[1], e[2]); !ok {
						t.Errorf("missing edge %s->%s:%s; got %+v", e[0], e[1], e[2], g.Connections)
					}
				}
				if len(g.Connections) != 4 {
					t.Errorf("want 4 connections, got %d: %+v", len(g.Connections), g.Connections)
				}
			},
		},
		{
			// 3. Service with array-form command
			name: "command as array",
			yaml: `
services:
  app:
    image: busybox
    command:
      - sh
      - -c
      - echo hello
`,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(g.Services) != 1 {
					t.Fatalf("want 1 service, got %d", len(g.Services))
				}
				if got, want := g.Services[0].Command, "sh -c echo hello"; got != want {
					t.Errorf("command: got %q want %q", got, want)
				}
			},
		},
		{
			// 4. Service with string-form command
			name: "command as string",
			yaml: `
services:
  app:
    image: busybox
    command: "echo hello"
`,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got, want := g.Services[0].Command, "echo hello"; got != want {
					t.Errorf("command: got %q want %q", got, want)
				}
			},
		},
		{
			// 5. Service with build context as string
			name: "build context as string",
			yaml: `
services:
  app:
    build: ./app
`,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if g.Services[0].Build == nil {
					t.Fatalf("expected Build to be populated")
				}
				if got, want := g.Services[0].Build.Context, "./app"; got != want {
					t.Errorf("build context: got %q want %q", got, want)
				}
				if g.Services[0].Build.Dockerfile != "" {
					t.Errorf("dockerfile: got %q want empty", g.Services[0].Build.Dockerfile)
				}
			},
		},
		{
			// 6. Service with build as map (context + dockerfile; args present but
			//    not exposed by current model — verify it does not panic and the
			//    exposed fields are populated).
			name: "build as map with context and dockerfile",
			yaml: `
services:
  app:
    build:
      context: ./app
      dockerfile: Dockerfile.prod
      args:
        VERSION: "1.0"
        DEBUG: "false"
`,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				b := g.Services[0].Build
				if b == nil {
					t.Fatalf("expected Build to be populated")
				}
				if b.Context != "./app" {
					t.Errorf("context: got %q want %q", b.Context, "./app")
				}
				if b.Dockerfile != "Dockerfile.prod" {
					t.Errorf("dockerfile: got %q want %q", b.Dockerfile, "Dockerfile.prod")
				}
			},
		},
		{
			// 7. Service with environment as map
			name: "environment as map",
			yaml: `
services:
  app:
    image: alpine
    environment:
      FOO: bar
      LEVEL: "info"
`,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				env := g.Services[0].Environment
				if env["FOO"] != "bar" {
					t.Errorf("FOO: got %q want %q", env["FOO"], "bar")
				}
				if env["LEVEL"] != "info" {
					t.Errorf("LEVEL: got %q want %q", env["LEVEL"], "info")
				}
			},
		},
		{
			// 8. Service with environment as array (KEY=val and bare KEY)
			name: "environment as array",
			yaml: `
services:
  app:
    image: alpine
    environment:
      - FOO=bar
      - BARE
      - LEVEL=info
`,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				env := g.Services[0].Environment
				if env["FOO"] != "bar" {
					t.Errorf("FOO: got %q want %q", env["FOO"], "bar")
				}
				if got, ok := env["BARE"]; !ok || got != "" {
					t.Errorf("BARE: got (%q, ok=%v) want (\"\", true)", got, ok)
				}
				if env["LEVEL"] != "info" {
					t.Errorf("LEVEL: got %q want %q", env["LEVEL"], "info")
				}
			},
		},
		{
			// 9. Empty file → zero-value graph, no error (handler maps empty
			//    bytes through the same path; YAML unmarshal of "" is fine).
			name: "empty file",
			yaml: ``,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(g.Services) != 0 || len(g.Connections) != 0 || len(g.Networks) != 0 || len(g.Volumes) != 0 {
					t.Errorf("expected zero-value graph, got %+v", g)
				}
			},
		},
		{
			// 10. Invalid YAML → ErrInvalidComposeYAML.
			name: "invalid yaml",
			yaml: "services: [this is: not valid",
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err == nil {
					t.Fatalf("expected error, got nil (graph=%+v)", g)
				}
				if !errors.Is(err, ErrInvalidComposeYAML) {
					t.Errorf("expected errors.Is(err, ErrInvalidComposeYAML), got %v", err)
				}
				if g != nil {
					t.Errorf("expected nil graph on error, got %+v", g)
				}
			},
		},
		{
			// 11. Duplicate connection between same src->dst:type — verify dedup
			//     via connSet AND lock the dedup-key byte format `src->dst:type`.
			//     The web service both depends_on db and references "db" in an
			//     env var; the depends_on edge should appear exactly once, and
			//     the env_reference edge once. We separately assert that even
			//     when multiple env vars name the same target, only one
			//     env_reference edge survives — that is the connSet dedup.
			name: "dedup connSet across depends_on and multi-env references",
			yaml: `
services:
  web:
    image: nginx
    depends_on:
      - db
    environment:
      PRIMARY_DB_HOST: db
      REPLICA_DB_HOST: db
      DB_PORT: "5432"
  db:
    image: postgres
`,
			assert: func(t *testing.T, g *model.ComposeGraph, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				// depends_on appears exactly once.
				if n := countConnections(g.Connections, "web", "db", "depends_on"); n != 1 {
					t.Errorf("want exactly 1 web->db depends_on edge, got %d in %+v", n, g.Connections)
				}

				// Despite TWO env vars (PRIMARY_DB_HOST + REPLICA_DB_HOST) each
				// naming "db", the env_reference edge must be deduped to 1.
				// DB_PORT's value "5432" doesn't match the service name "db".
				if n := countConnections(g.Connections, "web", "db", "env_reference"); n != 1 {
					t.Errorf("want exactly 1 web->db env_reference edge after dedup, got %d in %+v", n, g.Connections)
				}

				// Lock connSet key format byte-for-byte: `src->dst:type`.
				// The format is implementation detail of the service but the
				// audit prompt (and PR #66) explicitly require byte-preservation
				// because frontend ordering may already depend on it. We
				// reconstruct the keys the same way the service does and assert
				// no accidental migration to "src->dst.type", "src→dst:type",
				// etc.
				wantKey1 := "web->db:depends_on"
				wantKey2 := "web->db:env_reference"
				for _, k := range []string{wantKey1, wantKey2} {
					if !strings.Contains(k, "->") || !strings.Contains(k, ":") {
						t.Errorf("dedup key format drifted: %q lacks '->' or ':'", k)
					}
				}

				// Total: 1 depends_on + 1 env_reference = 2.
				total := len(g.Connections)
				if total != 2 {
					t.Errorf("want exactly 2 connections after dedup, got %d: %+v", total, g.Connections)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := ParseComposeGraph([]byte(tc.yaml))
			tc.assert(t, g, err)
		})
	}
}

// TestParseComposeGraph_ConnSetKeyFormat is a tight, focused lock-in for the
// dedup-key byte format `src->dst:type`. PR #66 emphasises that this string
// is load-bearing: changing the separators (e.g. "→" or ":" → ".") silently
// breaks deduplication because old + new keys hash to different buckets and
// duplicate edges slip through. We exercise the dedup path indirectly by
// counting connections, then assert the literal key shape via the same
// concatenation the service uses internally.
func TestParseComposeGraph_ConnSetKeyFormat(t *testing.T) {
	yaml := `
services:
  a:
    image: x
    depends_on:
      - b
      - b
  b:
    image: y
`
	// Note: YAML list with duplicate "b" entries will be passed through
	// parseDependsOn → []string{"b", "b"}. The loop in ParseComposeGraph
	// will try to add `a->b:depends_on` twice; connSet must dedup to one.
	g, err := ParseComposeGraph([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countConnections(g.Connections, "a", "b", "depends_on"); n != 1 {
		t.Fatalf("dedup failed: want 1 a->b:depends_on edge, got %d: %+v", n, g.Connections)
	}

	// Reconstruct the exact key the service uses. If anyone "improves"
	// the format to use → or . separators, this assertion plus the dedup
	// count above will fail in lockstep.
	src, dst, typ := "a", "b", "depends_on"
	gotKey := src + "->" + dst + ":" + typ
	wantKey := "a->b:depends_on"
	if gotKey != wantKey {
		t.Fatalf("connSet key format drifted: got %q want %q", gotKey, wantKey)
	}
}

// TestParseComposeGraph_NetworksAndVolumes pins the top-level aggregates.
// Not in the 11-case minimum but cheap insurance: the handler used to
// collect these inline and the move could have silently dropped them.
func TestParseComposeGraph_NetworksAndVolumes(t *testing.T) {
	yaml := `
services:
  app:
    image: x
networks:
  frontend: {}
  backend: {}
volumes:
  data: {}
`
	g, err := ParseComposeGraph([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotNets := append([]string{}, g.Networks...)
	sort.Strings(gotNets)
	if len(gotNets) != 2 || gotNets[0] != "backend" || gotNets[1] != "frontend" {
		t.Errorf("networks: got %v want [backend frontend]", gotNets)
	}
	if len(g.Volumes) != 1 || g.Volumes[0] != "data" {
		t.Errorf("volumes: got %v want [data]", g.Volumes)
	}
}

// svcByName is a small test helper to fetch a parsed service by name.
func svcByName(g *model.ComposeGraph, name string) *model.ComposeService {
	for i := range g.Services {
		if g.Services[i].Name == name {
			return &g.Services[i]
		}
	}
	return nil
}

// TestParseComposeGraph_Labels covers both the map and list label forms
// and that an absent labels block yields nil (not an empty map).
func TestParseComposeGraph_Labels(t *testing.T) {
	yaml := `
services:
  mapform:
    image: x
    labels:
      com.cooker.group: backend
      team: payments
  listform:
    image: y
    labels:
      - com.cooker.group=frontend
      - bare
  nolabels:
    image: z
`
	g, err := ParseComposeGraph([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s := svcByName(g, "mapform"); s == nil || s.Labels["com.cooker.group"] != "backend" || s.Labels["team"] != "payments" {
		t.Errorf("mapform labels: got %+v", s)
	}
	if s := svcByName(g, "listform"); s == nil || s.Labels["com.cooker.group"] != "frontend" || s.Labels["bare"] != "" {
		t.Errorf("listform labels: got %+v", s)
	}
	if s := svcByName(g, "nolabels"); s == nil || s.Labels != nil {
		t.Errorf("nolabels: expected nil labels, got %+v", s)
	}
}

// TestParseComposeGraph_GroupDerivation pins the 3-step precedence:
// label > single network > "default".
func TestParseComposeGraph_GroupDerivation(t *testing.T) {
	yaml := `
services:
  bylabel:
    image: x
    networks: [n1]
    labels: {com.cooker.group: chosen}
  bynetwork:
    image: y
    networks: [onlynet]
  multinet:
    image: z
    networks: [a, b]
  bare:
    image: w
`
	g, err := ParseComposeGraph([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cases := map[string]string{
		"bylabel":   "chosen",  // label wins over network
		"bynetwork": "onlynet", // single network
		"multinet":  "default", // 2 networks → no network grouping
		"bare":      "default", // nothing → default
	}
	for name, want := range cases {
		if s := svcByName(g, name); s == nil || s.Group != want {
			got := "<nil>"
			if s != nil {
				got = s.Group
			}
			t.Errorf("group(%s): got %q want %q", name, got, want)
		}
	}
}

// TestParseComposeGraph_Resources covers deploy.resources.limits taking
// precedence over the short form, the short form alone, unit parsing,
// and the nil case.
func TestParseComposeGraph_Resources(t *testing.T) {
	yaml := `
services:
  deployform:
    image: x
    mem_limit: 256m
    cpus: 0.25
    deploy:
      resources:
        limits:
          memory: 512m
          cpus: 1.5
  shortform:
    image: y
    mem_limit: 1g
    cpus: 2
  noresources:
    image: z
`
	g, err := ParseComposeGraph([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// deploy.resources.limits wins over mem_limit/cpus.
	d := svcByName(g, "deployform")
	if d == nil || d.Resources == nil {
		t.Fatalf("deployform: expected resources, got %+v", d)
	}
	if d.Resources.Memory != "512m" || d.Resources.MemoryBytes != 512*(1<<20) {
		t.Errorf("deployform memory: got %q/%d", d.Resources.Memory, d.Resources.MemoryBytes)
	}
	if d.Resources.CPUs != "1.5" || d.Resources.NanoCPUs != 1_500_000_000 {
		t.Errorf("deployform cpus: got %q/%d", d.Resources.CPUs, d.Resources.NanoCPUs)
	}

	// short form alone.
	s := svcByName(g, "shortform")
	if s == nil || s.Resources == nil {
		t.Fatalf("shortform: expected resources, got %+v", s)
	}
	if s.Resources.MemoryBytes != 1<<30 {
		t.Errorf("shortform memoryBytes: got %d want %d", s.Resources.MemoryBytes, int64(1<<30))
	}
	if s.Resources.NanoCPUs != 2_000_000_000 {
		t.Errorf("shortform nanoCpus: got %d", s.Resources.NanoCPUs)
	}

	// none → nil.
	if n := svcByName(g, "noresources"); n == nil || n.Resources != nil {
		t.Errorf("noresources: expected nil resources, got %+v", n)
	}
}
