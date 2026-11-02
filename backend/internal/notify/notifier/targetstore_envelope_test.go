package notifier

import "testing"

func TestUnknownTargetEnvelopeCannotFallBackToPlaintext(t *testing.T) {
	s := newSealingStore(t)
	for _, cfg := range []string{`null`, `[]`, `{"enc":"v2","data":"YQ=="}`, `{"enc":"v1"}`, `{"data":"YQ=="}`, `{"Enc":"v1","data":"YQ=="}`, `{"enc":"v1","data":null}`} {
		if _, err := s.openConfig([]byte(cfg)); err == nil {
			t.Fatal("malformed target envelope accepted")
		}
	}
}
