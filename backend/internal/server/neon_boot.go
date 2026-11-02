package server

import (
	"database/sql"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/provider/neon"
)

// Reuse the existing owned DB lifecycle. No background poller, provider read,
// deployment job or additional credential is created during boot.
func bootNeon(codec *crypto.Codec, deps *jobQueueDeps, shared *sql.DB) *neon.ConnectionService {
	if !codec.Active() {
		return nil
	}
	db := shared
	if deps != nil && deps.DB != nil {
		db = deps.DB
	}
	if db == nil {
		return nil
	}
	return &neon.ConnectionService{Store: &neon.PostgresConnections{DB: db}, Codec: codec, API: neon.NewClient()}
}
