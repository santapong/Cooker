package service

import (
	"errors"
	"strings"
	"testing"
)

const editFixture = `# acme stack — edited by cooker
services:
  web:
    image: nginx:1.27 # front door
    ports:
      - "8080:80"
    depends_on:
      - api

  api: # the core
    build: ./api
    image: acme/api:1.0
    environment:
      - DATABASE_URL=postgres://db/acme
      - LOG_LEVEL=info
    ports:
      - "9000:9000"

  # db — the store
  db:
    image: postgres:16
    environment:
      POSTGRES_DB: acme
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata: {}
`

func strp(s string) *string                       { return &s }
func listp(s ...string) *[]string                 { return &s }
func envp(m map[string]string) *map[string]string { return &m }

func TestPatchComposeService_RewritesOnlyTheTarget(t *testing.T) {
	out, err := PatchComposeService([]byte(editFixture), "api", ComposeServicePatch{
		Image:       strp("acme/api:1.1"),
		Ports:       listp("9000:9000", "9443:9443"),
		Environment: envp(map[string]string{"LOG_LEVEL": "debug", "DATABASE_URL": "postgres://db/acme", "TRACE": "1"}),
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	text := string(out)
	for _, want := range []string{
		"# acme stack — edited by cooker",         // head comment survives
		"image: nginx:1.27 # front door",          // untouched service, line comment intact
		"  api: # the core",                       // the edited service's own key line is kept verbatim
		"    build: ./api",                        // sibling keys of the edited service, original indent
		"    image: acme/api:1.1",                 //
		"      - DATABASE_URL=postgres://db/acme", // list style kept
		"      - LOG_LEVEL=debug",                 //
		"      - TRACE=1",                         //
		"      - \"9000:9000\"",                   // ports stay double-quoted
		"      - \"9443:9443\"",                   //
		"\n\n  # db — the store\n  db:\n",         // blank line + head comment of the next service survive
		"POSTGRES_DB: acme",                       //
		"pgdata: {}",                              //
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "acme/api:1.0") || strings.Contains(text, "LOG_LEVEL=info") {
		t.Errorf("old values still present:\n%s", text)
	}
	if got, want := strings.Count(text, "\n\n"), strings.Count(editFixture, "\n\n"); got != want {
		t.Errorf("blank lines: got %d, want %d:\n%s", got, want, text)
	}
	if !strings.HasSuffix(text, "pgdata: {}\n") {
		t.Errorf("trailing newline lost:\n%q", text[len(text)-20:])
	}
	// env keeps the file's order (DATABASE_URL, LOG_LEVEL) and appends the new key
	if strings.Index(text, "- DATABASE_URL") > strings.Index(text, "- LOG_LEVEL=debug") || strings.Index(text, "- LOG_LEVEL=debug") > strings.Index(text, "- TRACE=1") {
		t.Errorf("env order changed:\n%s", text)
	}
	// key order of the edited service is preserved: build, image, environment, ports
	if strings.Index(text, "build: ./api") > strings.Index(text, "image: acme/api:1.1") ||
		strings.Index(text, "image: acme/api:1.1") > strings.Index(text, "- DATABASE_URL") ||
		strings.Index(text, "- LOG_LEVEL=debug") > strings.Index(text, "9443:9443") {
		t.Errorf("key order changed:\n%s", text)
	}

	g, err := ParseComposeGraph(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for _, s := range g.Services {
		if s.Name != "api" {
			continue
		}
		if s.Image != "acme/api:1.1" || len(s.Ports) != 2 || s.Environment["LOG_LEVEL"] != "debug" || s.Environment["TRACE"] != "1" {
			t.Errorf("re-parsed api = %+v", s)
		}
	}
}

func TestPatchComposeService_MappingEnvAndRemovals(t *testing.T) {
	out, err := PatchComposeService([]byte(editFixture), "db", ComposeServicePatch{
		Image:       strp(""),
		Ports:       listp(),
		Environment: envp(map[string]string{"POSTGRES_DB": "acme", "POSTGRES_PASSWORD": "s3cret"}),
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	text := string(out)
	if strings.Contains(text, "postgres:16") {
		t.Errorf("empty image should remove the key:\n%s", text)
	}
	if !strings.Contains(text, "POSTGRES_PASSWORD: s3cret") || !strings.Contains(text, "POSTGRES_DB: acme") {
		t.Errorf("mapping style env not kept:\n%s", text)
	}
	if strings.Index(text, "POSTGRES_DB: acme") > strings.Index(text, "POSTGRES_PASSWORD: s3cret") {
		t.Errorf("existing mapping key should stay first:\n%s", text)
	}
	if strings.Contains(text, "- POSTGRES_") {
		t.Errorf("env turned into a list:\n%s", text)
	}
	// ports absent before and empty now → no key added
	if strings.Count(text, "ports:") != 2 {
		t.Errorf("ports key count = %d, want 2 (web + api):\n%s", strings.Count(text, "ports:"), text)
	}
	// the last service before a top-level key: the blank line and `volumes:` block are untouched
	if !strings.Contains(text, "      - pgdata:/var/lib/postgresql/data\n\nvolumes:\n  pgdata: {}\n") {
		t.Errorf("tail of the file changed:\n%s", text)
	}
}

func TestPatchComposeService_NewKeysAndNilFields(t *testing.T) {
	src := "services:\n  worker:\n    build: ./worker\n"
	out, err := PatchComposeService([]byte(src), "worker", ComposeServicePatch{
		Environment: envp(map[string]string{"QUEUE": "jobs"}),
		Ports:       listp("7000:7000"),
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "build: ./worker") || !strings.Contains(text, "QUEUE: jobs") || !strings.Contains(text, "7000:7000") {
		t.Errorf("unexpected output:\n%s", text)
	}
	if strings.Contains(text, "image:") {
		t.Errorf("nil image must not add a key:\n%s", text)
	}
}

func TestPatchComposeService_FlowStyleFallsBackToReencode(t *testing.T) {
	src := "services:\n  web: {image: nginx:1, ports: [\"80:80\"]}\n  db: {image: postgres:16}\n"
	out, err := PatchComposeService([]byte(src), "web", ComposeServicePatch{Image: strp("nginx:2")})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	g, err := ParseComposeGraph(out)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, out)
	}
	var images []string
	for _, s := range g.Services {
		images = append(images, s.Image)
	}
	joined := strings.Join(images, ",")
	if !strings.Contains(joined, "nginx:2") || !strings.Contains(joined, "postgres:16") {
		t.Errorf("images = %v\n%s", images, out)
	}
}

func TestPatchComposeService_LastServiceBeforeEOF(t *testing.T) {
	src := "services:\n  only:\n    image: a:1\n"
	out, err := PatchComposeService([]byte(src), "only", ComposeServicePatch{Image: strp("a:2"), Ports: listp("1:1")})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if string(out) != "services:\n  only:\n    image: a:2\n    ports:\n      - \"1:1\"\n" {
		t.Errorf("unexpected output:\n%q", out)
	}
}

func TestPatchComposeService_Errors(t *testing.T) {
	if _, err := PatchComposeService([]byte(editFixture), "ghost", ComposeServicePatch{Image: strp("x")}); !errors.Is(err, ErrComposeServiceNotFound) {
		t.Errorf("unknown service: err = %v, want ErrComposeServiceNotFound", err)
	}
	if _, err := PatchComposeService([]byte("version: '3'\n"), "web", ComposeServicePatch{Image: strp("x")}); !errors.Is(err, ErrComposeServiceNotFound) {
		t.Errorf("no services key: err = %v, want ErrComposeServiceNotFound", err)
	}
	if _, err := PatchComposeService([]byte("services: [\n"), "web", ComposeServicePatch{Image: strp("x")}); !errors.Is(err, ErrInvalidComposeYAML) {
		t.Errorf("broken yaml: err = %v, want ErrInvalidComposeYAML", err)
	}
}
