package service

import (
	"testing"

	"github.com/santapong/cooker/internal/model"
)

// Synthesized app pipelines stamp the deployer's CacheRef onto build
// stages — and only build stages — iff a cache repo is configured.
func TestAppDeployer_CacheSpec(t *testing.T) {
	if (&AppDeployer{}).cacheSpec() != nil {
		t.Error("empty CacheRef must produce a nil spec")
	}
	got := (&AppDeployer{CacheRef: "reg.example.com/cooker/cache"}).cacheSpec()
	if got == nil || got.Mode != "registry" || got.Ref != "reg.example.com/cooker/cache" {
		t.Errorf("cacheSpec() = %+v", got)
	}
}

func TestSynthesizePipeline_StampsCacheOnBuildOnly(t *testing.T) {
	app := &model.App{ID: "a1", Name: "shop"}
	spec := &model.CacheSpec{Mode: "registry", Ref: "reg.example.com/cooker/cache"}

	p, _ := synthesizePipeline(app, nil, "/work", "reg/shop:1", spec, synthOpts{})
	for _, s := range p.Stages {
		switch s.Type {
		case model.StageTypeBuild:
			if s.Config.Cache != spec {
				t.Errorf("build stage cache = %+v, want the configured spec", s.Config.Cache)
			}
		default:
			if s.Config.Cache != nil {
				t.Errorf("%s stage unexpectedly carries a cache spec", s.Type)
			}
		}
	}

	p, _ = synthesizePipeline(app, nil, "/work", "reg/shop:1", nil, synthOpts{})
	for _, s := range p.Stages {
		if s.Config.Cache != nil {
			t.Errorf("nil spec must leave %s stage cache nil", s.Type)
		}
	}
}
