package notifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Delivery never includes target configuration, receiver response bodies, or
// executor errors. A delivered record means a receiver accepted the request;
// it is not proof that a human read it.
type Delivery struct {
	ID                string     `json:"id"`
	EventID           string     `json:"eventId"`
	EventType         EventType  `json:"eventType"`
	TargetID          string     `json:"targetId"`
	TargetVersion     int64      `json:"targetVersion"`
	Kind              string     `json:"kind"`
	State             string     `json:"state"`
	Attempts          int        `json:"attempts"`
	MaxAttempts       int        `json:"maxAttempts"`
	NextAttemptAt     time.Time  `json:"nextAttemptAt"`
	LeaseUntil        *time.Time `json:"leaseUntil,omitempty"`
	LastCode          string     `json:"lastCode,omitempty"`
	LastHTTPStatus    int        `json:"lastHttpStatus,omitempty"`
	DuplicatePossible bool       `json:"duplicatePossible"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	owner             string
	event             Event
}

type DeliveryQuery struct {
	State, TargetID, RunID string
	Limit, Offset          int
}

type DeliveryPage struct {
	Items                   []Delivery `json:"items"`
	Total                   int        `json:"total"`
	Mode                    string     `json:"mode"`
	PendingSourceJobs       int        `json:"pendingSourceJobs"`
	PendingSourceRepairJobs int        `json:"pendingSourceRepairJobs"`
}

type DeliveryReader interface {
	ListDeliveries(context.Context, DeliveryQuery) (DeliveryPage, error)
	GetDelivery(context.Context, string) (Delivery, error)
}

var ErrDeliveryNotFound = errors.New("notification delivery not found")
var safeIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_:.-]{1,200}$`)

func digestID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + hex.EncodeToString(sum[:])
}

// SafeEvent deliberately excludes operator-entered names, namespaces, URLs,
// and raw errors. This path has no authoritative execution redactor context;
// passing those values through would make failure notifications a secret leak.
// Detailed errors stay in Cooker's existing authorized/redacted run views.
func SafeEvent(event Event) (Event, error) {
	status := ""
	switch event.Type {
	case EventRunSucceeded, EventDeploySucceeded:
		status = "success"
	case EventRunFailed, EventDeployFailed, EventBuildFailed, EventCanaryFailed:
		status = "failed"
	case EventRunCancelled, EventDeployCancelled:
		status = "cancelled"
	case EventCanaryPromoted:
		status = "promoted"
	case EventCanaryAborted:
		status = "aborted"
	default:
		return Event{}, errors.New("notification event type is unsupported")
	}
	for _, id := range []string{event.RunID, event.AppID, event.PipelineID, event.CanaryID} {
		if id != "" && !safeIdentifier.MatchString(id) {
			return Event{}, errors.New("notification identity is invalid")
		}
	}
	safe := Event{Type: event.Type, RunID: event.RunID, AppID: event.AppID,
		PipelineID: event.PipelineID, CanaryID: event.CanaryID, Status: status, Timestamp: event.Timestamp}
	if safe.PipelineID != "" {
		safe.PipelineName = "Pipeline " + safe.PipelineID
	}
	if safe.AppID != "" {
		safe.AppName = "App " + safe.AppID
		safe.Environment = "recorded target"
	}
	if safe.Timestamp.IsZero() {
		safe.Timestamp = time.Now().UTC()
	}
	identity := ""
	if safe.RunID != "" {
		identity = "terminal:" + safe.RunID
	}
	if event.Type == EventCanaryPromoted || event.Type == EventCanaryAborted || event.Type == EventCanaryFailed {
		identity = ""
		if safe.CanaryID != "" {
			identity = "canary:" + safe.CanaryID + ":" + string(safe.Type)
		}
	}
	if identity != "" {
		safe.ID = digestID("ne_", identity)
	}
	if status == "failed" {
		safe.Error = "Operation failed. Inspect the run in Cooker for details."
	}
	return safe, nil
}

// SendFailure contains only reviewed codes. Neither adapter errors nor remote
// response bodies are suitable for logging or persistence.
type SendFailure struct {
	Code       string
	HTTPStatus int
	Retryable  bool
	Uncertain  bool
}

func (e *SendFailure) Error() string { return "notification send: " + e.Code }

func invalidConfig() error { return &SendFailure{Code: "invalid_configuration"} }

func receiverFailure(status int) error {
	return &SendFailure{Code: "receiver_http_rejected", HTTPStatus: status, Retryable: status == 429 || status >= 500}
}

func uncertainSend() error { return &SendFailure{Code: "send_outcome_unknown", Uncertain: true} }

func safeFailure(err error) *SendFailure {
	var failure *SendFailure
	if errors.As(err, &failure) {
		// Defensive: third-party adapters cannot smuggle arbitrary text via Code.
		switch failure.Code {
		case "invalid_configuration", "receiver_http_rejected", "send_outcome_unknown", "adapter_unavailable", "target_read_unavailable":
			copy := *failure
			return &copy
		}
	}
	return &SendFailure{Code: "send_outcome_unknown", Uncertain: true}
}

// boundedSend also bounds adapters such as legacy SMTP that do not honor ctx.
// The underlying call may still finish later; callers must record uncertainty
// rather than retrying that external operation.
func boundedSend(ctx context.Context, n Notifier, target Target, event Event) error {
	ch := make(chan error, 1)
	go func() {
		defer func() {
			if recover() != nil {
				ch <- uncertainSend()
			}
		}()
		ch <- n.Send(ctx, target, event)
	}()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return uncertainSend()
	}
}

func deliveryMode() string {
	return "Durable after event acceptance. Native terminal jobs repair missed acceptance; uncommitted inline/canary events have no replay. HTTP retries can duplicate messages. Uncertain sends require inspection; target changes cannot revoke an in-flight send."
}

func deliveryBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 30 * time.Second
	}
	return 2 * time.Minute
}

func deliveryID(eventID, targetID string) string {
	return digestID("nd_", fmt.Sprintf("%s\x00%s", eventID, targetID))
}
