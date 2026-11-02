package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
)

// AutomationEnvelope is deliberately conservative: source code may change,
// but selected Compose inputs, referenced env files, and Dockerfiles must
// retain their approved bytes. Values and file contents are never persisted.
type AutomationEnvelope struct {
	Hash   string              `json:"hash"`
	Commit string              `json:"commit"`
	Files  []string            `json:"files"`
	Graph  *model.ComposeGraph `json:"graph"`
}

func (d *AppDetector) InspectAutomation(ctx context.Context, app *model.App, env map[string]string) (*AutomationEnvelope, error) {
	if app == nil || app.BuildPlan == nil || app.BuildPlan.Kind != model.BuildPlanCompose || !github.ValidCommit(app.BuildPlan.Commit) {
		return nil, fmt.Errorf("automation requires a selected Compose plan and full commit")
	}
	if app.BuildPlan.EnvironmentScope != "service" {
		return nil, fmt.Errorf("choose explicit configuration permissions for each service before enabling automation")
	}
	ctx, cancel := context.WithTimeout(ctx, appDetectTimeout)
	defer cancel()
	opts := github.CloneOptions{Repo: app.GitHubRepo, Branch: app.Branch, Commit: app.BuildPlan.Commit, InstallationID: app.BuildPlan.InstallationID, Depth: 1}
	var root string
	var err error
	if d.SourceCheckout != nil && app.BuildPlan.SourceConnectionID != "" {
		root, err = d.SourceCheckout(ctx, app, opts.Commit, io.Discard)
	} else {
		root, err = d.cloneFn(ctx, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve source for automation review")
	}
	defer os.RemoveAll(root)
	sha, err := github.HeadCommit(ctx, root)
	if err != nil || sha != opts.Commit {
		return nil, fmt.Errorf("automation checkout does not match the selected commit")
	}
	resolved, err := loadRepositoryCompose(ctx, root, app, env)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect deployment configuration")
	}
	if len(resolved.Diagnostics) > 0 {
		return nil, fmt.Errorf("deployment configuration has unresolved diagnostics; inspect the App before enabling automation")
	}
	if d.CheckExecution != nil {
		hasBuild := false
		for _, s := range resolved.Graph.Services {
			hasBuild = hasBuild || s.Build != nil && !s.External
		}
		if err := d.CheckExecution(app, hasBuild); err != nil {
			return nil, err
		}
	}
	files := append([]string{}, app.BuildPlan.Files...)
	if len(files) == 0 {
		files = []string{app.BuildPlan.Path}
	}
	for _, f := range resolved.BuildFiles {
		files = append(files, f.Dockerfile)
	}
	// The loader resolves env files inside the checkout. Include their bytes in
	// the envelope even though the preview removes resolved values.
	files = append(files, resolved.InputFiles...)
	hashes := map[string]string{}
	for _, f := range files {
		path, err := repositoryPath(root, root, f)
		if err != nil {
			return nil, fmt.Errorf("review input is unavailable")
		}
		b, err := readSourceFile(path)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		h := sha256.Sum256(b)
		hashes[filepath.ToSlash(rel)] = hex.EncodeToString(h[:])
	}
	// Inspection redaction hides env/command values, so raw input hashes are
	// mandatory; hashing only the redacted graph would miss privilege changes.
	b, err := json.Marshal(hashes)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	paths := make([]string, 0, len(hashes))
	for f := range hashes {
		paths = append(paths, f)
	}
	sort.Strings(paths)
	redactComposeGraph(resolved.Graph)
	return &AutomationEnvelope{Hash: hex.EncodeToString(sum[:]), Commit: sha, Files: paths, Graph: resolved.Graph}, nil
}

func automationPlanHash(app *model.App, environmentVersion, connectionVersion int) string {
	copy := *app
	copy.WebhookSecret = nil
	copy.HasWebhook = false
	copy.AutoDeploy = false
	copy.Branch = ""
	copy.HealthStatus = ""
	copy.HealthCheckedAt = nil
	copy.HealthMessage = ""
	copy.DeployedURL = ""
	if app.BuildPlan != nil {
		p := *app.BuildPlan
		p.Commit = ""
		copy.BuildPlan = &p
	}
	b, _ := json.Marshal(struct {
		App                                   model.App
		EnvironmentVersion, ConnectionVersion int
	}{copy, environmentVersion, connectionVersion})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
