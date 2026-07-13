package builder

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Cache wiring tests: every adapter must add its cache transport flags
// when CacheSpec selects registry/oci + ref, and add nothing otherwise.

func TestCacheSpec_Enabled(t *testing.T) {
	cases := []struct {
		name string
		spec CacheSpec
		want bool
	}{
		{"zero value", CacheSpec{}, false},
		{"disabled", CacheSpec{Mode: "disabled", Ref: "reg.example.com/c"}, false},
		{"registry no ref", CacheSpec{Mode: "registry"}, false},
		{"registry with ref", CacheSpec{Mode: "registry", Ref: "reg.example.com/c"}, true},
		{"oci with ref", CacheSpec{Mode: "oci", Ref: "reg.example.com/c"}, true},
	}
	for _, tc := range cases {
		if got := tc.spec.enabled(); got != tc.want {
			t.Errorf("%s: enabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestKaniko_BuildJob_CacheFlags(t *testing.T) {
	k := newKanikoForTest(t, "")

	withCache := k.buildJob(Request{
		ContextDir: "/src", Tags: []string{"t"},
		Cache: CacheSpec{Mode: "registry", Ref: "reg.example.com/app/cache"},
	})
	args := withCache.Spec.Template.Spec.Containers[0].Args
	assertContains(t, args, "--cache=true")
	assertContains(t, args, "--cache-repo=reg.example.com/app/cache")

	for name, spec := range map[string]CacheSpec{
		"no cache": {},
		"disabled": {Mode: "disabled", Ref: "reg.example.com/app/cache"},
	} {
		job := k.buildJob(Request{ContextDir: "/src", Tags: []string{"t"}, Cache: spec})
		for _, a := range job.Spec.Template.Spec.Containers[0].Args {
			if strings.HasPrefix(a, "--cache") {
				t.Errorf("%s: unexpected cache flag %q", name, a)
			}
		}
	}
}

func TestBuildah_BuildJob_CacheFlags(t *testing.T) {
	b := &Buildah{
		cfg: BuildahConfig{
			Namespace:     "test-ns",
			Image:         "buildah:test",
			StorageDriver: "vfs",
		},
	}

	withCache := b.buildJob(Request{
		ContextDir: "/src", Tags: []string{"t"},
		Cache: CacheSpec{Mode: "registry", Ref: "reg.example.com/app/cache"},
	})
	// Cache flags travel as discrete argv entries appended after the
	// static script (the $@ mechanism); a single env var would not
	// word-split under the script's IFS=$'\n'.
	cmd := withCache.Spec.Template.Spec.Containers[0].Command
	assertContains(t, cmd, "--layers")
	assertContains(t, cmd, "--cache-from=reg.example.com/app/cache")
	assertContains(t, cmd, "--cache-to=reg.example.com/app/cache")

	noCache := b.buildJob(Request{ContextDir: "/src", Tags: []string{"t"}})
	for _, a := range noCache.Spec.Template.Spec.Containers[0].Command {
		if strings.HasPrefix(a, "--cache") || a == "--layers" {
			t.Errorf("unexpected cache flag %q without CacheSpec", a)
		}
	}
}

func TestSolveOptions_Cache(t *testing.T) {
	base := Request{ContextDir: "/src", Tags: []string{"reg.example.com/app:v1"}}

	plain := solveOptions(base)
	if len(plain.CacheImports) != 0 || len(plain.CacheExports) != 0 {
		t.Fatalf("expected no cache entries without CacheSpec; got %+v / %+v",
			plain.CacheImports, plain.CacheExports)
	}

	req := base
	req.Cache = CacheSpec{Mode: "registry", Ref: "reg.example.com/app/cache"}
	opt := solveOptions(req)
	if len(opt.CacheImports) != 1 || len(opt.CacheExports) != 1 {
		t.Fatalf("expected one import + one export, got %d / %d",
			len(opt.CacheImports), len(opt.CacheExports))
	}
	if opt.CacheImports[0].Type != "registry" || opt.CacheImports[0].Attrs["ref"] != req.Cache.Ref {
		t.Errorf("import = %+v, want registry ref %q", opt.CacheImports[0], req.Cache.Ref)
	}
	if _, ok := opt.CacheExports[0].Attrs["mode"]; ok {
		t.Error("mode=max must only be set when Inline is requested")
	}

	req.Cache.Mode = "oci"
	req.Cache.Inline = true
	opt = solveOptions(req)
	if opt.CacheImports[0].Attrs["oci-mediatypes"] != "true" {
		t.Error("oci mode should set oci-mediatypes on the import")
	}
	if opt.CacheExports[0].Attrs["mode"] != "max" {
		t.Error("Inline should map to mode=max on the export")
	}
	if _, ok := opt.CacheImports[0].Attrs["mode"]; ok {
		t.Error("mode=max must not leak onto the import attrs")
	}
}

func TestDockerSock_IgnoresCache(t *testing.T) {
	// Bin "true" exits 0 for any argv, so Build succeeds without a
	// real docker daemon; the cache note must land on LogWriter.
	var buf bytes.Buffer
	d := &DockerSock{Bin: "true"}
	_, err := d.Build(context.Background(), Request{
		ContextDir: "/src", Tags: []string{"t"},
		LogWriter: &buf,
		Cache:     CacheSpec{Mode: "registry", Ref: "reg.example.com/c"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(buf.String(), "cache: registry layer cache not supported") {
		t.Errorf("expected unsupported-cache note in log, got %q", buf.String())
	}
}

func assertContains(t *testing.T, list []string, want string) {
	t.Helper()
	for _, a := range list {
		if a == want {
			return
		}
	}
	t.Errorf("missing %q in %v", want, list)
}
