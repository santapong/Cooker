package store

import "context"

// NativeTriggerAdmissionGuard serializes prepare/admit/recover for one intent.
// This does not claim exactly-once external deployment; queue/worker fences
// remain separate. A process crash releases the PostgreSQL connection lock.
type NativeTriggerAdmissionGuard interface {
	WithNativeTriggerAdmission(context.Context, string, func(context.Context) error) error
}
