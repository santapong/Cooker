package memory

// paginate applies the store List contract (limit <= 0 = all, negative
// offset = 0) to an already-sorted slice, mirroring the Postgres
// LIMIT/OFFSET semantics. Callers must sort before slicing so pages
// are deterministic.
func paginate[T any](s []T, limit, offset int) []T {
	if offset > 0 {
		if offset >= len(s) {
			return []T{}
		}
		s = s[offset:]
	}
	if limit > 0 && len(s) > limit {
		s = s[:limit]
	}
	return s
}
