package postgres

import "encoding/base64"

// base64StdEncode / base64StdDecode wrap stdlib base64 with names
// scoped to this package so secrets-at-rest logic reads uniformly.
func base64StdEncode(b []byte) string          { return base64.StdEncoding.EncodeToString(b) }
func base64StdDecode(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
