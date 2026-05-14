package jobqueue

import (
	"strconv"
	"sync/atomic"
)

// idCounter is a process-local monotonic suffix that combines with
// the wall-clock nanosecond timestamp to produce a unique job ID
// without an external dependency. Collisions across replicas are
// avoided by the Postgres primary key constraint — the caller's
// retry on a conflict (unlikely with nanosecond + counter) will
// produce a fresh ID.
var idCounter uint64

func nextIDSeq() uint64 { return atomic.AddUint64(&idCounter, 1) }

func formatID(unixNano int64, seq uint64) string {
	return "job_" + strconv.FormatInt(unixNano, 36) + "_" + strconv.FormatUint(seq, 36)
}
