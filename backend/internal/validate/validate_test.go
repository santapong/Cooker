package validate

import (
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestStageType(t *testing.T) {
	for _, ok := range []model.StageType{model.StageTypeBuild, model.StageTypeDeploy, model.StageTypeTest, model.StageTypePush, model.StageTypeApproval, model.StageTypeCustom, model.StageTypeGitOpsCommit} {
		if err := StageType(ok); err != nil {
			t.Errorf("StageType(%q) unexpected err: %v", ok, err)
		}
	}
	if err := StageType("nonsense"); err == nil {
		t.Error("StageType should reject unknown")
	}
}

func TestGitHubRepo(t *testing.T) {
	good := []string{"foo/bar", "user-1/some.repo", "Owner-1/repo-name"}
	bad := []string{"", "noslash", "/leadslash", "trail/", "spaces /repo", "foo/bar/baz"}
	for _, s := range good {
		if err := GitHubRepo(s); err != nil {
			t.Errorf("GitHubRepo(%q) unexpected err: %v", s, err)
		}
	}
	for _, s := range bad {
		if err := GitHubRepo(s); err == nil {
			t.Errorf("GitHubRepo(%q) should reject", s)
		}
	}
}

func TestDockerTag(t *testing.T) {
	good := []string{"v1", "1.0.0", "latest", "_dev", "feature_x"}
	bad := []string{"", "-leadhyphen", strings.Repeat("a", 200), "tag with spaces"}
	for _, s := range good {
		if err := DockerTag(s); err != nil {
			t.Errorf("DockerTag(%q) unexpected err: %v", s, err)
		}
	}
	for _, s := range bad {
		if err := DockerTag(s); err == nil {
			t.Errorf("DockerTag(%q) should reject", s)
		}
	}
}

func TestDockerTagsWithRegistry(t *testing.T) {
	if err := DockerTags([]string{"registry.example.com/app:v1.2.3"}); err != nil {
		t.Errorf("registry-qualified tag should pass: %v", err)
	}
}

func TestGitRefName(t *testing.T) {
	good := []string{"", "main", "feature/x", "release-1.2.3"}
	bad := []string{"foo..bar", "trailing/", " withspace"}
	for _, s := range good {
		if err := GitRefName("branch", s); err != nil {
			t.Errorf("GitRefName(%q) unexpected err: %v", s, err)
		}
	}
	for _, s := range bad {
		if err := GitRefName("branch", s); err == nil {
			t.Errorf("GitRefName(%q) should reject", s)
		}
	}
}

func TestHTTPSURL(t *testing.T) {
	if err := HTTPSURL("u", "http://localhost:5173", false); err != nil {
		t.Errorf("dev HTTP allowed when requireHTTPS=false: %v", err)
	}
	if err := HTTPSURL("u", "http://example.com", true); err == nil {
		t.Error("requireHTTPS should reject http")
	}
	if err := HTTPSURL("u", "https://example.com", true); err != nil {
		t.Errorf("https accepted: %v", err)
	}
}

func TestHostKind(t *testing.T) {
	if err := HostKind(model.HostKindDocker); err != nil {
		t.Error("docker should be valid")
	}
	if err := HostKind("kvm"); err == nil {
		t.Error("unknown kind should reject")
	}
}

func TestNameLength(t *testing.T) {
	if err := Name("name", ""); err == nil {
		t.Error("empty name should reject")
	}
	if err := Name("name", strings.Repeat("a", MaxNameLen+1)); err == nil {
		t.Error("over-cap name should reject")
	}
}
