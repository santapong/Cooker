package server

import (
	"context"
	"database/sql"

	"github.com/santapong/cooker/internal/config"
	"github.com/santapong/cooker/internal/crypto"
	"github.com/santapong/cooker/internal/provider/vercel"
)

// Vercel synchronization is independent of the deployment queue and AWS/GCP
// feature flags. A database plus active encryption key are required for durable
// connections. The caller closes a dedicated pool only when one was opened.
func bootVercel(ctx context.Context, cfg *config.Config, codec *crypto.Codec, jobDeps *jobQueueDeps) (*vercel.Service, *sql.DB, error) {
	if cfg.DatabaseURL == "" || !codec.Active() {
		return nil, nil, nil
	}
	var db *sql.DB
	var owned *sql.DB
	if jobDeps != nil && jobDeps.DB != nil {
		db = jobDeps.DB
	} else {
		var err error
		db, err = sql.Open("postgres", cfg.DatabaseURL)
		if err != nil {
			return nil, nil, err
		}
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(1)
		if err = db.PingContext(ctx); err != nil {
			_ = db.Close()
			return nil, nil, err
		}
		owned = db
	}
	return &vercel.Service{Store: &vercel.Postgres{DB: db}, Codec: codec, API: vercel.NewClient()}, owned, nil
}
