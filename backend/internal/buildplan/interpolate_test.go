package buildplan

import (
	"reflect"
	"strings"
	"testing"
)

func resolver() OutputResolver {
	return OutputResolver{Stages: map[string]map[string]string{
		"build": {"digest": "sha256:abc123", "tag": "v1"},
		"push":  {"ref": "reg/app:v1"},
	}}
}

func TestInterpolate_DigestSubstitution(t *testing.T) {
	got, err := Interpolate("reg/app@${stages.build.digest}", resolver())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "reg/app@sha256:abc123"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInterpolate_ImagePassthrough(t *testing.T) {
	// ${IMAGE} is existing runtime templating; it must pass through
	// untouched and must NOT produce an "unknown stage" error.
	in := "${IMAGE}"
	got, err := Interpolate(in, resolver())
	if err != nil {
		t.Fatalf("unexpected error for ${IMAGE}: %v", err)
	}
	if got != in {
		t.Errorf("expected ${IMAGE} to pass through verbatim, got %q", got)
	}
}

func TestInterpolate_MixedTokens(t *testing.T) {
	// A stages token and an ${IMAGE} token in the same string: the
	// stages token is resolved, ${IMAGE} is left verbatim.
	got, err := Interpolate("${IMAGE}@${stages.build.digest}", resolver())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "${IMAGE}@sha256:abc123"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInterpolate_UnknownStage(t *testing.T) {
	_, err := Interpolate("${stages.nope.digest}", resolver())
	if err == nil {
		t.Fatal("expected error for unknown stage")
	}
	if !strings.Contains(err.Error(), `unknown stage "nope"`) {
		t.Errorf("unexpected error text: %v", err)
	}
}

func TestInterpolate_UnknownKey(t *testing.T) {
	_, err := Interpolate("${stages.build.missing}", resolver())
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), `stage "build" has no output "missing"`) {
		t.Errorf("unexpected error text: %v", err)
	}
}

func TestInterpolate_MultipleTokensOneString(t *testing.T) {
	got, err := Interpolate("${stages.build.tag}-${stages.build.digest}", resolver())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "v1-sha256:abc123"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInterpolate_NoTokens(t *testing.T) {
	in := "plain-string-no-tokens"
	got, err := Interpolate(in, resolver())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != in {
		t.Errorf("expected unchanged, got %q", got)
	}
}

func TestInterpolate_MalformedTokenLeftVerbatim(t *testing.T) {
	// ${stages.x} is missing the .key segment, so it is NOT a valid
	// token: left verbatim, no error.
	in := "prefix-${stages.x}-suffix"
	got, err := Interpolate(in, resolver())
	if err != nil {
		t.Fatalf("unexpected error for malformed token: %v", err)
	}
	if got != in {
		t.Errorf("expected malformed token left verbatim, got %q", got)
	}
}

func TestInterpolate_FirstErrorWins(t *testing.T) {
	// Two failing tokens; the first one's error is returned.
	_, err := Interpolate("${stages.alpha.x}${stages.beta.y}", resolver())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("expected first token (alpha) to win, got %v", err)
	}
}

func TestInterpolateSlice(t *testing.T) {
	in := []string{"${stages.build.digest}", "literal", "${IMAGE}"}
	got, err := InterpolateSlice(in, resolver())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"sha256:abc123", "literal", "${IMAGE}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// New copy, not the same backing array.
	if &in[0] == &got[0] {
		t.Error("expected a new slice, got the same backing array")
	}
}

func TestInterpolateSlice_NilInNilOut(t *testing.T) {
	got, err := InterpolateSlice(nil, resolver())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil out for nil in, got %v", got)
	}
}

func TestInterpolateSlice_Error(t *testing.T) {
	_, err := InterpolateSlice([]string{"ok", "${stages.nope.x}"}, resolver())
	if err == nil {
		t.Fatal("expected error from slice element")
	}
}

func TestInterpolateMap(t *testing.T) {
	in := map[string]string{"REF": "${stages.push.ref}", "PLAIN": "x", "IMG": "${IMAGE}"}
	got, err := InterpolateMap(in, resolver())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"REF": "reg/app:v1", "PLAIN": "x", "IMG": "${IMAGE}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestInterpolateMap_NilInNilOut(t *testing.T) {
	got, err := InterpolateMap(nil, resolver())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil out for nil in, got %v", got)
	}
}

func TestInterpolateMap_Error(t *testing.T) {
	_, err := InterpolateMap(map[string]string{"k": "${stages.build.missing}"}, resolver())
	if err == nil {
		t.Fatal("expected error from map value")
	}
}

func TestReferences(t *testing.T) {
	got := References("${stages.build.digest}/${stages.push.ref}/${stages.build.tag}/${IMAGE}")
	want := []string{"build", "push"} // distinct, first-seen order
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReferences_None(t *testing.T) {
	if got := References("no tokens ${IMAGE} ${stages.x}"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
