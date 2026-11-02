package jobqueue

import "testing"

func TestPayloadDigestSurvivesJSONBWithoutRoundingIdentity(t *testing.T) {
	a, err := PayloadDigest([]byte(`{"version":1,"identity":9007199254740993,"nested":{"a":1,"b":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := PayloadDigest([]byte(`{ "nested": {"b":2, "a":1}, "identity":9007199254740993, "version":1 }`))
	if err != nil || a != b {
		t.Fatal("JSONB representation changed receipt", err)
	}
	c, _ := PayloadDigest([]byte(`{"version":1,"identity":9007199254740992,"nested":{"a":1,"b":2}}`))
	if a == c {
		t.Fatal("large identities were rounded")
	}
}
