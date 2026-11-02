package deployer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/go-containerregistry/pkg/name"
)

const runtimeReceiptByteLimit = 8 * 1024
const dockerRuntimeRunLabel = "io.cooker.run-id"
const dockerRuntimeStageLabel = "io.cooker.stage-id"

// Explicit safe fields only: never Config.Env, all labels, raw inspect, state
// or health. Capture pins the exact start ID rather than a reusable name.
const runtimeReceiptInspectFormat = `[` +
	`{{json .Id}},{{json .Name}},{{json .Image}},{{json .Config.Image}},` +
	`{{json (index .Config.Labels "io.cooker.run-id")}},` +
	`{{json (index .Config.Labels "io.cooker.stage-id")}}]`

var runtimeReceiptFullID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var runtimeReceiptName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// A diagnostic/capture bound must not affect the successful Docker operation.
// Keep a bounded prefix and remember overflow; do not retain the remainder.
type runtimeReceiptBuffer struct {
	data      bytes.Buffer
	truncated bool
}

func (b *runtimeReceiptBuffer) Len() int       { return b.data.Len() }
func (b *runtimeReceiptBuffer) String() string { return b.data.String() }
func (b *runtimeReceiptBuffer) Bytes() []byte  { return b.data.Bytes() }

func (b *runtimeReceiptBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := runtimeReceiptByteLimit - b.Len()
	if len(p) > remaining {
		b.truncated = true
		p = p[:remaining]
	}
	_, _ = b.data.Write(p)
	return n, nil
}

func runtimeReceiptText(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.TrimSpace(value) == value &&
		!strings.ContainsFunc(value, unicode.IsControl)
}

func (d *DockerRun) captureRuntimeReceipt(ctx context.Context, req Request, effectiveName string, env []string, started *runtimeReceiptBuffer) *RuntimeReceipt {
	id := strings.TrimSpace(started.String())
	runID, stageID := req.Labels[dockerRuntimeRunLabel], req.Labels[dockerRuntimeStageLabel]
	if started.truncated || !runtimeReceiptFullID.MatchString(id) ||
		!runtimeReceiptName.MatchString(effectiveName) || !runtimeReceiptText(runID, 128) || !runtimeReceiptText(stageID, 128) ||
		!runtimeReceiptText(req.Image, 1024) {
		return nil
	}
	if _, err := name.ParseReference(req.Image); err != nil {
		return nil
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(inspectCtx, d.bin(), "container", "inspect", "--format", runtimeReceiptInspectFormat, id)
	// Bound pipe draining as well as the parent process. A CLI wrapper may
	// exit while a descendant still holds its output descriptors open.
	cmd.WaitDelay = 250 * time.Millisecond
	data := &runtimeReceiptBuffer{}
	cmd.Env, cmd.Stdout, cmd.Stderr = env, data, io.Discard
	if err := cmd.Run(); err != nil || data.truncated {
		return nil // no raw CLI diagnostics or unsafe response enter the warning
	}
	var fields []string
	if json.Unmarshal(data.Bytes(), &fields) != nil || len(fields) != 6 ||
		fields[0] != id || fields[1] != "/"+effectiveName || fields[3] != req.Image ||
		fields[4] != runID || fields[5] != stageID ||
		!strings.HasPrefix(fields[2], "sha256:") || !runtimeReceiptFullID.MatchString(strings.TrimPrefix(fields[2], "sha256:")) {
		return nil
	}
	return &RuntimeReceipt{Version: 1, Runtime: "docker", RunID: runID, StageID: stageID,
		ContainerID: id, Name: effectiveName, ImageReference: req.Image,
		ImageID: fields[2], CapturedAt: time.Now().UTC()}
}
