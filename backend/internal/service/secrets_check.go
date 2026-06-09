package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/santapong/cooker/internal/secrets"
	"github.com/santapong/cooker/internal/secrets/keepsave"
)

// secretsCheckTimeout bounds the connectivity probe. Long enough for
// a cold TLS handshake to a remote manager, short enough that the
// settings page gets an answer while the admin is still looking at it.
const secretsCheckTimeout = 10 * time.Second

// SecretsCheckResult is the outcome of one connectivity probe
// (roadmap M5 / F12). It reveals the backend kind and reachability
// only — never key names or values; List results are discarded.
type SecretsCheckResult struct {
	Backend   string `json:"backend"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latencyMs"`
	Error     string `json:"error,omitempty"`
}

// CheckSecrets probes the configured secrets backend by listing the
// given environment's keys (the cheapest authenticated read every
// adapter implements). Errors are classified into operator-actionable
// buckets: auth-failed, unreachable, or the raw message.
func CheckSecrets(ctx context.Context, mgr secrets.Manager, backend, envID string) SecretsCheckResult {
	res := SecretsCheckResult{Backend: backend}
	cctx, cancel := context.WithTimeout(ctx, secretsCheckTimeout)
	defer cancel()

	start := time.Now()
	_, err := mgr.List(cctx, envID)
	res.LatencyMS = int64(time.Since(start) / time.Millisecond)
	if err == nil {
		res.OK = true
		return res
	}
	res.Error = classifySecretsErr(err)
	return res
}

// classifySecretsErr maps probe failures to short operator-facing
// strings. KeepSave surfaces typed HTTP errors; the other adapters
// fall through to the network/timeout checks or the raw message.
func classifySecretsErr(err error) string {
	var httpErr *keepsave.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "authentication failed — check the backend credentials"
		}
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return "backend unreachable — probe timed out"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return "backend unreachable — " + opErr.Err.Error()
	}
	return err.Error()
}
