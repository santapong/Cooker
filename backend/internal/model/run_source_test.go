package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testAcceptedSourcePlan() *PipelineSourcePlan {
	return &PipelineSourcePlan{Version: 1, PipelineID: "native", PipelineVersion: 3, Source: GitSourceIdentity{Provider: "github", ConnectionID: "approved", Host: "github.com", RepositoryID: "123", Repository: "owned/native"}, FullRef: "refs/tags/v1", CommitSHA: strings.Repeat("a", 40), ObjectSHA: strings.Repeat("b", 40), ConnectionVersion: 4, EnvironmentVersions: map[string]int{"private-env-canary": 9}, InputFiles: []SourceInputHash{{Path: "private-source-canary", SHA256: strings.Repeat("c", 64), Present: true}}}
}

func TestRunSourceProjectsAcceptedMetadataWithoutCheckoutClaim(t *testing.T) {
	source, err := RunSourceFromAcceptedPlan(testAcceptedSourcePlan())
	if err != nil || source == nil || source.CheckedOutAt != nil {
		t.Fatal("projection claimed checkout or lost accepted metadata", source, err)
	}
	b, err := json.Marshal(source)
	if err != nil || strings.Contains(string(b), "canary") || strings.Contains(string(b), "environmentVersions") || strings.Contains(string(b), "inputFiles") || source.ObjectSHA != strings.Repeat("b", 40) {
		t.Fatal("private plan escaped run projection", string(b), err)
	}
	if legacy, err := RunSourceFromAcceptedPlan(nil); err != nil || legacy != nil {
		t.Fatal("missing legacy source was manufactured", legacy, err)
	}
	plan := testAcceptedSourcePlan()
	plan.Source.Host = "secret-canary@github.com"
	if _, err := RunSourceFromAcceptedPlan(plan); !errors.Is(err, ErrInvalidRunSource) || strings.Contains(err.Error(), "canary") {
		t.Fatal("credential-like host was accepted or echoed", err)
	}
}

func TestPipelineRunSourceCloneAndImmutableMerge(t *testing.T) {
	base, err := RunSourceFromAcceptedPlan(testAcceptedSourcePlan())
	if err != nil {
		t.Fatal(err)
	}
	checked := time.Now().UTC()
	base.CheckedOutAt = &checked
	r := &PipelineRun{Source: base}
	copy := r.Clone()
	copy.Source.Source.Repository = "other/native"
	*copy.Source.CheckedOutAt = checked.Add(time.Hour)
	if r.Source.Source.Repository != "owned/native" || !r.Source.CheckedOutAt.Equal(checked) {
		t.Fatal("source clone aliases original")
	}
	unchecked := base.Clone()
	unchecked.CheckedOutAt = nil
	for _, next := range []*PipelineRunSource{nil, unchecked, base.Clone()} {
		merged, err := MergeRunSource(base, next)
		if err != nil || merged == base || merged.CheckedOutAt == base.CheckedOutAt || !merged.CheckedOutAt.Equal(checked) {
			t.Fatal("compatible update erased/aliased checkout receipt", err)
		}
	}
	confirmed, err := MergeRunSource(unchecked, base)
	if err != nil || confirmed.CheckedOutAt == nil {
		t.Fatal("verified worker observation could not be added", err)
	}
	for _, poison := range []string{"commit", "digest", "checked-time"} {
		t.Run(poison, func(t *testing.T) {
			next := base.Clone()
			switch poison {
			case "commit":
				next.CommitSHA = strings.Repeat("d", 40)
			case "digest":
				next.SourcePlanDigest = strings.Repeat("d", 64)
			case "checked-time":
				time := checked.Add(time.Second)
				next.CheckedOutAt = &time
			}
			if _, err := MergeRunSource(base, next); !errors.Is(err, ErrInvalidRunSource) {
				t.Fatal("changed source lineage replaced receipt", err)
			}
		})
	}
}
