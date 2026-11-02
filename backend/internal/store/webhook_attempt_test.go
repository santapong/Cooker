package store

import (
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/model"
)

func TestWebhookJournalInputAndOutcomeAllowlist(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("x", 257), " id ", "id\tsecret", "id\nsecret", string([]byte{255})} {
		if SafeWebhookTransportID(value) {
			t.Fatal("invalid transport identity accepted")
		}
	}
	for _, outcome := range [][2]string{{"failed", "synthetic-secret-canary"}, {"accepted", "invalid_json"}, {"received", "processing"}, {"", ""}} {
		if ValidateWebhookAttemptOutcome(outcome[0], outcome[1]) == nil {
			t.Fatal("invalid journal outcome accepted", outcome)
		}
	}
	a := &model.WebhookAttempt{ID: "a", Provider: "github", ConnectionID: "connection", Status: "received", ReasonCode: "processing"}
	if err := ValidateWebhookAttempt(a); err != nil {
		t.Fatal(err)
	}
	a.ProviderDeliveryID = "bad\ttransport"
	if err := ValidateWebhookAttempt(a); err == nil {
		t.Fatal("control-bearing provider identity persisted")
	}
}
