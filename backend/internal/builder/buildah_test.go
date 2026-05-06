package builder

import (
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

// TestBuildah_NoShellInjection asserts that user-controllable
// inputs (Dockerfile, ContextDir, BuildArgs, Tags) are passed via
// Container.Env / positional args rather than interpolated into
// the shell command. A hostile pipeline definition can no longer
// break out by injecting shell metacharacters.
func TestBuildah_NoShellInjection(t *testing.T) {
	b := &Buildah{
		cfg: BuildahConfig{
			Namespace:     "test-ns",
			Image:         "buildah:test",
			StorageDriver: "vfs",
			PollInterval:  10 * time.Millisecond,
			Client:        fake.NewSimpleClientset(),
		},
		client: fake.NewSimpleClientset(),
	}

	// A request whose every user-controlled field embeds shell
	// metacharacters. If any of these characters land inside the
	// command string concatenated with other tokens, the buildah
	// invocation breaks out into a separate shell command.
	req := Request{
		Dockerfile: "; touch /tmp/owned ; #",
		ContextDir: "/ctx ; touch /tmp/ctx-owned",
		Tags:       []string{`reg/img:tag"; touch /tmp/tag-owned ; "`},
		BuildArgs: map[string]string{
			"INJECT": "value\"; touch /tmp/arg-owned; \"",
		},
	}
	job := b.buildJob(req)

	c := job.Spec.Template.Spec.Containers[0]

	// The Command must NOT contain the user values in interpolated
	// form. The first three elements are /bin/sh, -c, the static
	// script. The remaining elements are positional args (the
	// --build-arg flags); those are fine as separate argv entries.
	if len(c.Command) < 3 || c.Command[0] != "/bin/sh" || c.Command[1] != "-c" {
		t.Fatalf("expected Command to start with /bin/sh -c <script>, got %v", c.Command)
	}
	script := c.Command[2]
	for _, danger := range []string{
		"; touch /tmp/owned",
		"/tmp/ctx-owned",
		"/tmp/tag-owned",
		"/tmp/arg-owned",
	} {
		if strings.Contains(script, danger) {
			t.Errorf("user input %q leaked into script:\n%s", danger, script)
		}
	}

	// Each user value must appear in an Env var (where shell
	// re-tokenisation can't reach it).
	envByName := map[string]string{}
	for _, e := range c.Env {
		envByName[e.Name] = e.Value
	}
	if envByName["DOCKERFILE"] != req.Dockerfile {
		t.Errorf("DOCKERFILE env should hold dockerfile verbatim, got %q", envByName["DOCKERFILE"])
	}
	if envByName["CONTEXT_DIR"] != req.ContextDir {
		t.Errorf("CONTEXT_DIR env should hold contextDir verbatim, got %q", envByName["CONTEXT_DIR"])
	}
	if envByName["TAGS"] == "" || !strings.Contains(envByName["TAGS"], req.Tags[0]) {
		t.Errorf("TAGS env should contain the requested tag, got %q", envByName["TAGS"])
	}

	// Build args must travel as positional argv entries
	// (--build-arg=key=value), not be embedded in the script.
	foundBuildArg := false
	for _, a := range c.Command[4:] { // [3] is argv0 ("buildah-job")
		if strings.HasPrefix(a, "--build-arg=INJECT=") && a == "--build-arg=INJECT="+req.BuildArgs["INJECT"] {
			foundBuildArg = true
		}
	}
	if !foundBuildArg {
		t.Errorf("--build-arg=INJECT=<value> should appear as a positional argv entry, got %v", c.Command[3:])
	}
}
