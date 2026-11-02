package jobqueue

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

// PayloadDigest is stable across PostgreSQL JSONB key ordering/whitespace.
// UseNumber avoids rounding large integer identities through float64.
func PayloadDigest(payload []byte) (string, error) {
	d := json.NewDecoder(bytes.NewReader(payload))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return "", err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("invalid trailing plan data")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
