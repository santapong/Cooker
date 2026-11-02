package jobqueue

import "database/sql"

// RegistryAdmissionDatabaseMatches proves pool identity without inspecting a
// DSN. Distinct pools may address the same database, but cannot prove it here;
// production explicitly binds authority metadata stores to the queue's pool.
func (p *Postgres) RegistryAdmissionDatabaseMatches(db *sql.DB) bool {
	return p != nil && p.db != nil && p.db == db
}
