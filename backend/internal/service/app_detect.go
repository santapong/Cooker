package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/santapong/cooker/internal/build/buildplan"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/source/github"
)

// appDetectTimeout bounds the shallow clone + detect pass. A hung
// remote must fail the request, not pin the handler goroutine.
const appDetectTimeout = 30 * time.Second

// AppDetector inspects a repository before the App exists, so the
// New-App wizard can pre-select a build recipe. The clone function is
// swappable for tests.
type AppDetector struct {
	cloneFn func(ctx context.Context, opts github.CloneOptions) (string, error)
}

// NewAppDetector returns a detector backed by the real GitHub clone.
func NewAppDetector() *AppDetector {
	return &AppDetector{cloneFn: github.Clone}
}

// NewAppDetectorWithClone swaps the clone for tests (mirrors
// kube.NewWithClientset). cloneFn must return a directory the
// detector may delete.
func NewAppDetectorWithClone(cloneFn func(ctx context.Context, opts github.CloneOptions) (string, error)) *AppDetector {
	return &AppDetector{cloneFn: cloneFn}
}

// DetectBuild shallow-clones repo@branch, runs buildplan.Detect on the
// tree, and maps root-level language markers onto one of the wizard's
// recipe ids ("go", "node-static", "worker"). The recipe is advisory;
// the plan is the same value an App deploy would auto-detect. The
// working tree is removed before returning.
func (d *AppDetector) DetectBuild(ctx context.Context, repo, branch string) (*model.BuildPlan, string, error) {
	ctx, cancel := context.WithTimeout(ctx, appDetectTimeout)
	defer cancel()
	workdir, err := d.cloneFn(ctx, github.CloneOptions{Repo: repo, Branch: branch, Depth: 1})
	if err != nil {
		return nil, "", fmt.Errorf("app detect: clone: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(workdir); rmErr != nil {
			slog.Warn("app-detect: rm workdir failed", "workdir", workdir, "err", rmErr)
		}
	}()
	return buildplan.Detect(workdir), suggestRecipe(workdir), nil
}

// suggestRecipe maps repo-root language markers onto the New-App
// wizard's recipe ids. Checked in priority order; "worker" is the
// generic fallback card.
func suggestRecipe(dir string) string {
	if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
		return "go"
	}
	if st, err := os.Stat(filepath.Join(dir, "package.json")); err == nil && !st.IsDir() {
		return "node-static"
	}
	return "worker"
}
