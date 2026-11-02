package model

import (
	"regexp"
	"strings"
	"time"
	"unicode"
)

// RuntimeBindingUnavailable deliberately carries no CLI or private metadata.
const RuntimeBindingUnavailable = "runtime_receipt_unavailable"

// RuntimeReceipt records one actual Docker start. It does not assert current
// existence, readiness, health, worker ownership or future log authorization.
type RuntimeReceipt struct {
	Version        int       `json:"version"`
	Runtime        string    `json:"runtime"`
	RunID          string    `json:"runId"`
	StageID        string    `json:"stageId"`
	ContainerID    string    `json:"containerId"`
	Name           string    `json:"name"`
	ImageReference string    `json:"imageReference"`
	ImageID        string    `json:"imageId"`
	CapturedAt     time.Time `json:"capturedAt"`
}

var receiptContainerID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var receiptName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// Valid checks only the bounded shape. The producer must separately prove
// run/stage/name/image equality and withhold any known secret values.
func (r *RuntimeReceipt) Valid() bool {
	if r == nil || r.Version != 1 || r.Runtime != "docker" || r.CapturedAt.IsZero() {
		return false
	}
	return receiptContainerID.MatchString(r.ContainerID) &&
		strings.HasPrefix(r.ImageID, "sha256:") && receiptContainerID.MatchString(strings.TrimPrefix(r.ImageID, "sha256:")) &&
		receiptName.MatchString(r.Name) && receiptText(r.RunID, 128) && receiptText(r.StageID, 128) &&
		receiptText(r.ImageReference, 1024) && !strings.ContainsAny(r.ImageReference, " \t") && !strings.Contains(r.ImageReference, "://")
}

func receiptText(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.TrimSpace(value) == value &&
		!strings.ContainsFunc(value, unicode.IsControl)
}
