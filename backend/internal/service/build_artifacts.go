package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/santapong/cooker/internal/build/artifact"
	"github.com/santapong/cooker/internal/build/registryauth"
)

type buildArtifactsKey struct{}
type buildArtifacts struct {
	mu    sync.Mutex
	byTag map[string]*artifact.Image
	all   map[*artifact.Image]bool
}

func withBuildArtifacts(ctx context.Context) (context.Context, func()) {
	b := &buildArtifacts{byTag: make(map[string]*artifact.Image), all: make(map[*artifact.Image]bool)}
	return context.WithValue(ctx, buildArtifactsKey{}, b), func() {
		for a := range b.all {
			_ = a.Close()
		}
	}
}
func keepBuildArtifact(ctx context.Context, a *artifact.Image, tags []string) error {
	if a == nil {
		return nil
	}
	b, _ := ctx.Value(buildArtifactsKey{}).(*buildArtifacts)
	if b == nil {
		_ = a.Close()
		return fmt.Errorf("build artifact requires a run-scoped owner")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.all[a] = true
	for _, tag := range tags {
		if old := b.byTag[tag]; old != nil && old != a {
			return fmt.Errorf("multiple build stages produced image tag %q", tag)
		}
	}
	for _, tag := range tags {
		b.byTag[tag] = a
	}
	return nil
}
func findBuildArtifact(ctx context.Context, tag string) *artifact.Image {
	b, _ := ctx.Value(buildArtifactsKey{}).(*buildArtifacts)
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byTag[tag]
}

func WithRegistryAuth(resolve registryauth.Resolver) Option {
	return func(e *Executor) { e.registryAuth = resolve }
}
