package jobqueue

import "context"

// WithClaimPayload is an additive domain-write capability. Both immutable
// payload identity and the running owner are checked under the claim mutex.
func (m *Memory) WithClaimPayload(ctx context.Context, owner, id, kind, digest string, write func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	observed, err := PayloadDigest(j.Payload)
	if err != nil || digest == "" || observed != digest || j.Kind != kind || j.Status != StatusRunning || j.LockedBy != owner {
		return ErrNotLocked
	}
	return write()
}
