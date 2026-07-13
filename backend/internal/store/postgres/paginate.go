package postgres

// limitArg converts the store List contract's "limit <= 0 means
// unbounded" into the SQL parameter for `LIMIT $n`: Postgres treats
// LIMIT NULL as no limit, so a nil interface value is passed through.
func limitArg(limit int) interface{} {
	if limit > 0 {
		return limit
	}
	return nil
}

// clampOffset maps negative offsets to 0 (the contract treats them as
// "start from the beginning").
func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}
