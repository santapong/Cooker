package buildplan

import (
	"fmt"
	"regexp"
)

// outputRef matches a single ${stages.<id>.<key>} interpolation token.
//
// The grammar is deliberately strict (DR-3): stage IDs allow
// [A-Za-z0-9_-] and output keys allow [A-Za-z0-9_]. Any ${...} that
// does not match this exact shape — including existing runtime
// templating like ${IMAGE} or a malformed ${stages.x} with no key — is
// left verbatim and is NOT an interpolation error here. Those tokens are
// resolved (or ignored) by other layers; this engine only owns the
// stages accessor.
var outputRef = regexp.MustCompile(`\$\{stages\.([A-Za-z0-9_-]+)\.([A-Za-z0-9_]+)\}`)

// OutputResolver resolves ${stages.<id>.<key>} references against completed
// upstream stage outputs.
type OutputResolver struct {
	Stages map[string]map[string]string // stageID -> (outputKey -> value)
}

// Interpolate replaces every ${stages.<id>.<key>} token in s with the
// resolved upstream output value. Every other character — including
// other ${...} tokens such as ${IMAGE} — is left verbatim.
//
// It returns an error if a referenced stage is unknown, or if a known
// stage has no such output key. The first such error wins (later tokens
// are not reported once one fails).
func Interpolate(s string, r OutputResolver) (string, error) {
	var firstErr error
	out := outputRef.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			return match
		}
		// FindStringSubmatch is guaranteed to match here: match came
		// from outputRef itself, so the two capture groups are present.
		groups := outputRef.FindStringSubmatch(match)
		stageID, key := groups[1], groups[2]
		outputs, ok := r.Stages[stageID]
		if !ok {
			firstErr = fmt.Errorf("buildplan: unknown stage %q referenced in ${stages.%s.%s}", stageID, stageID, key)
			return match
		}
		val, ok := outputs[key]
		if !ok {
			firstErr = fmt.Errorf("buildplan: stage %q has no output %q", stageID, key)
			return match
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// InterpolateSlice returns a NEW slice with Interpolate applied to each
// element. A nil input yields a nil output. The first element error wins.
func InterpolateSlice(in []string, r OutputResolver) ([]string, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]string, len(in))
	for i, v := range in {
		iv, err := Interpolate(v, r)
		if err != nil {
			return nil, err
		}
		out[i] = iv
	}
	return out, nil
}

// InterpolateMap returns a NEW map with Interpolate applied to each
// value (keys are copied verbatim). A nil input yields a nil output.
// The first value error wins.
func InterpolateMap(in map[string]string, r OutputResolver) (map[string]string, error) {
	if in == nil {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		iv, err := Interpolate(v, r)
		if err != nil {
			return nil, err
		}
		out[k] = iv
	}
	return out, nil
}

// References returns the distinct upstream stage IDs referenced by
// ${stages.<id>.<key>} tokens in s, in first-seen order. It performs no
// resolution and never errors — it is a static scanner used by pipeline
// validation to assert that every referenced stage is a declared
// ancestor. Output-key existence is intentionally NOT checked here
// (keys only exist at run time).
func References(s string) []string {
	matches := outputRef.FindAllStringSubmatch(s, -1)
	if matches == nil {
		return nil
	}
	var ids []string
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		id := m[1]
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}
