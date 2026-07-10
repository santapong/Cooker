package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/santapong/cooker/internal/build/builder"
	"github.com/santapong/cooker/internal/build/buildplan"
	"github.com/santapong/cooker/internal/deploy/deployer"
	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/build/pusher"
)

// Output caps (dag-adaptation-2026.md §7.3, DR-2). A single output value
// is truncated to outputKeyMax bytes; the running per-stage total is
// bounded at outputStageMax bytes, after which further keys are dropped
// and a "_truncated" marker is set.
const (
	outputKeyMax   = 4 << 10  // 4 KiB per value
	outputStageMax = 32 << 10 // 32 KiB per stage
)

// deriveBuildOutputs produces the executor's baseline outputs for a build
// stage from the adapter Result. Empty fields are omitted so a downstream
// ${stages.<id>.digest} on a backend that reports no digest fails loud
// (unknown output) rather than resolving to "".
func deriveBuildOutputs(res builder.Result) map[string]string {
	out := map[string]string{}
	if res.ImageID != "" {
		out["digest"] = res.ImageID
	}
	if len(res.Tags) > 0 {
		out["tags"] = strings.Join(res.Tags, ",")
		out["tag"] = res.Tags[0]
	}
	return out
}

// derivePushOutputs produces the executor's baseline outputs for a push
// stage. target is the fully-qualified destination ref the executor
// resolved (registry + repository).
func derivePushOutputs(res pusher.Result, target string) map[string]string {
	out := map[string]string{}
	if res.Digest != "" {
		out["digest"] = res.Digest
	}
	if target != "" {
		out["ref"] = target
	}
	return out
}

// deriveDeployOutputs produces the executor's baseline outputs for a
// deploy stage from the applied-resources list.
func deriveDeployOutputs(res deployer.Result) map[string]string {
	out := map[string]string{}
	if len(res.AppliedResources) > 0 {
		out["resources"] = strings.Join(res.AppliedResources, ",")
	}
	return out
}

// hasDisallowedControl reports whether v contains a control character
// other than a horizontal tab. Outputs are interpolated into config
// fields (and may surface in logs / the UI); newlines, NULs and other
// control bytes are rejected wholesale to avoid downstream injection.
func hasDisallowedControl(v string) bool {
	for _, r := range v {
		if r == '\t' {
			continue
		}
		// C0 controls (incl. CR/LF/NUL/ESC) and DEL.
		if r < 0x20 || r == 0x7f {
			return true
		}
		// C1 controls (0x80-0x9f) and the Unicode line/paragraph
		// separators (U+2028/U+2029) have no legitimate place in an output
		// value and can corrupt log/terminal/JSON-line/UI rendering.
		// Reject wholesale (defence in depth, audit finding L1).
		if (r >= 0x80 && r <= 0x9f) || r == '\u2028' || r == '\u2029' {
			return true
		}
	}
	return false
}

// applyStageOutputs merges the executor-derived baseline outputs with any
// adapter-emitted outputs (adapter wins on key collision) and stores the
// sanitized, capped result on sr.Outputs. Allocation is lazy: if nothing
// survives sanitization, sr.Outputs is left nil.
//
// Enforcement (dag-adaptation-2026.md §7.3 / DR-2):
//   - a value containing a disallowed control character is rejected (not
//     stored) and sets sr.Outputs["_invalid"]="true";
//   - a value longer than outputKeyMax bytes is truncated to outputKeyMax
//     with a marker appended and sets sr.Outputs["_truncated"]="true";
//   - once the running total exceeds outputStageMax bytes no further keys
//     are added and sr.Outputs["_truncated"]="true" is set.
func applyStageOutputs(sr *model.StageRun, derived, adapter map[string]string) {
	if sr == nil {
		return
	}
	// Merge derived first, then overlay adapter so adapter values win.
	merged := make(map[string]string, len(derived)+len(adapter))
	for k, v := range derived {
		merged[k] = v
	}
	for k, v := range adapter {
		merged[k] = v
	}
	if len(merged) == 0 {
		return
	}

	// Deterministic iteration so the cap cutoff (and which keys land
	// before the total is exhausted) is stable across runs.
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	const truncMarker = "...[truncated]"
	total := 0
	for _, k := range keys {
		v := merged[k]
		if hasDisallowedControl(v) {
			ensureOutputs(sr)["_invalid"] = "true"
			continue
		}
		if len(v) > outputKeyMax {
			v = v[:outputKeyMax] + truncMarker
			ensureOutputs(sr)["_truncated"] = "true"
		}
		// Stop adding once the running total would exceed the per-stage
		// cap. Marker keys ("_invalid"/"_truncated") are tiny and exempt.
		if total+len(k)+len(v) > outputStageMax {
			ensureOutputs(sr)["_truncated"] = "true"
			break
		}
		ensureOutputs(sr)[k] = v
		total += len(k) + len(v)
	}
}

// ensureOutputs lazily allocates sr.Outputs and returns it.
func ensureOutputs(sr *model.StageRun) map[string]string {
	if sr.Outputs == nil {
		sr.Outputs = map[string]string{}
	}
	return sr.Outputs
}

// collectStageOutputs builds the resolver view over completed ancestor
// stages: stageID -> (outputKey -> value), for ancestors that succeeded and
// actually produced outputs. allowed is the set of stage IDs that are
// ancestors of the stage being resolved (precomputed in Execute).
//
// Concurrency: this runs inside the runner task closure for a stage at DAG
// level N. We MUST read only ancestors. An ancestor completed at a level
// < N, and pkg/dagrunner joins each level with a sync.WaitGroup before
// starting the next (runner.go Run: wg.Wait() per level), so that barrier
// gives a happens-before edge and the read is race-free. Reading a
// non-ancestor — in particular a sibling at the SAME level — would read its
// StageRun.Status while its goroutine is still writing it, an unsynchronised
// data race (caught by TestExecutor_Outputs_ParallelSiblingsNoRace under
// -race). The allowed filter is what keeps this safe.
func collectStageOutputs(m map[string]*model.StageRun, allowed map[string]bool) map[string]map[string]string {
	out := make(map[string]map[string]string, len(allowed))
	// Iterate the (typically small) allowed/ancestor set and look each up
	// in the stage map, rather than scanning the whole map on every call —
	// the latter is O(stages) per stage, i.e. O(stages^2) across a run (F7,
	// docs/proposals/run-state-concurrency-2026.md). The ancestor-only read
	// stays the race-safety invariant documented above: allowed is built
	// from ancestors, joined by the per-level WaitGroup barrier before this
	// runs, so reading their StageRuns here is race-free.
	for id := range allowed {
		sr := m[id]
		if sr == nil || sr.Status != model.RunStatusSuccess || len(sr.Outputs) == 0 {
			continue
		}
		cp := make(map[string]string, len(sr.Outputs))
		for k, v := range sr.Outputs {
			cp[k] = v
		}
		out[id] = cp
	}
	return out
}

// interpolateStageConfig returns a shallow copy of s with its
// ${stages.<id>.<key>} references resolved against r in the data-bearing
// StageConfig string fields. The original Stage / StageConfig is never
// mutated (the copy is what the adapter dispatch uses).
//
// Script is intentionally not interpolated — it is exec'd; outputs are
// data, not code. HelmValues is skipped: it is a nested interface{} tree,
// out of scope for this primitive.
//
// On the first field error the copy is discarded and (nil, error) is
// returned; the caller treats that as a (non-retryable) stage failure.
func interpolateStageConfig(s *model.Stage, r buildplan.OutputResolver) (*model.Stage, error) {
	if s == nil {
		return nil, fmt.Errorf("buildplan: nil stage")
	}
	cp := *s
	cfg := s.Config // value copy of the config struct

	type strField struct {
		name string
		ptr  *string
	}
	strFields := []strField{
		{"registry", &cfg.Registry},
		{"repository", &cfg.Repository},
		{"image", &cfg.Image},
		{"context", &cfg.Context},
		{"dockerfile", &cfg.Dockerfile},
		{"manifestPath", &cfg.ManifestPath},
		{"helmChart", &cfg.HelmChart},
		{"namespace", &cfg.Namespace},
		{"gitopsRepo", &cfg.GitOpsRepo},
		{"gitopsBranch", &cfg.GitOpsBranch},
		{"gitopsPath", &cfg.GitOpsPath},
		{"gitopsMessage", &cfg.GitOpsMessage},
		{"gitopsContent", &cfg.GitOpsContent},
	}
	for _, f := range strFields {
		v, err := buildplan.Interpolate(*f.ptr, r)
		if err != nil {
			return nil, fmt.Errorf("svc: interpolate %s: %w", f.name, err)
		}
		*f.ptr = v
	}

	var err error
	if cfg.Tags, err = buildplan.InterpolateSlice(cfg.Tags, r); err != nil {
		return nil, fmt.Errorf("svc: interpolate tags: %w", err)
	}
	if cfg.Command, err = buildplan.InterpolateSlice(cfg.Command, r); err != nil {
		return nil, fmt.Errorf("svc: interpolate command: %w", err)
	}
	if cfg.Platforms, err = buildplan.InterpolateSlice(cfg.Platforms, r); err != nil {
		return nil, fmt.Errorf("svc: interpolate platforms: %w", err)
	}
	if cfg.BuildArgs, err = buildplan.InterpolateMap(cfg.BuildArgs, r); err != nil {
		return nil, fmt.Errorf("svc: interpolate buildArgs: %w", err)
	}
	if cfg.Env, err = buildplan.InterpolateMap(cfg.Env, r); err != nil {
		return nil, fmt.Errorf("svc: interpolate env: %w", err)
	}

	cp.Config = cfg
	return &cp, nil
}
