package deployer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeDeployOnlyReviewedServiceWithReadiness(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "docker-test")
	output := filepath.Join(root, "arguments")
	t.Setenv("COOKER_TEST_ARGUMENTS", output)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$COOKER_TEST_ARGUMENTS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Compose{Bin: bin}).Deploy(context.Background(), Request{Kind: KindCompose, Name: "shop-production", ComposeFile: "/tmp/reviewed.yaml", ComposeService: "api"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	args := string(data)
	for _, want := range []string{"compose\n-p\nshop-production", "--no-build", "--no-deps", "--wait", "--wait-timeout\n300", "api\n"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q in %s", want, args)
		}
	}
	if strings.Contains(args, "remove-orphans") {
		t.Fatal("per-service deployment can delete another branch's workloads")
	}
}
