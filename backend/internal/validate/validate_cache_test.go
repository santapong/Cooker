package validate

import (
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestCacheSpec(t *testing.T) {
	ok := []struct {
		name string
		spec *model.CacheSpec
	}{
		{"nil", nil},
		{"zero mode", &model.CacheSpec{}},
		{"disabled", &model.CacheSpec{Mode: "disabled"}},
		{"registry host path", &model.CacheSpec{Mode: "registry", Ref: "reg.example.com/org/app"}},
		{"registry with tag", &model.CacheSpec{Mode: "registry", Ref: "reg.example.com/org/app:buildcache"}},
		{"registry with port", &model.CacheSpec{Mode: "oci", Ref: "localhost:5000/cooker/cache"}},
	}
	for _, tc := range ok {
		if err := CacheSpec(tc.spec); err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
	}

	bad := []struct {
		name string
		spec *model.CacheSpec
	}{
		{"unknown mode", &model.CacheSpec{Mode: "s3", Ref: "reg/app"}},
		{"registry without ref", &model.CacheSpec{Mode: "registry"}},
		{"no path component", &model.CacheSpec{Mode: "registry", Ref: "justahost"}},
		// Shell-safety gate: these refs reach builder argv.
		{"whitespace", &model.CacheSpec{Mode: "registry", Ref: "reg.example.com/a b"}},
		{"semicolon", &model.CacheSpec{Mode: "registry", Ref: "reg.example.com/a;rm"}},
		{"dollar", &model.CacheSpec{Mode: "registry", Ref: "reg.example.com/$HOME/c"}},
		{"backtick", &model.CacheSpec{Mode: "registry", Ref: "reg.example.com/`id`/c"}},
		{"quote", &model.CacheSpec{Mode: "registry", Ref: `reg.example.com/a"b`}},
		{"uppercase repo", &model.CacheSpec{Mode: "registry", Ref: "reg.example.com/Org/App"}},
	}
	for _, tc := range bad {
		if err := CacheSpec(tc.spec); err == nil {
			t.Errorf("%s: expected rejection, got nil", tc.name)
		}
	}
}
