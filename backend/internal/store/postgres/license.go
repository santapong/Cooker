package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// activeLicenseID is the fixed primary-key sentinel for the single
// installed license row. The licenses table holds at most one row
// (migration 024 enforces this with a one-row CHECK); keying on a
// constant makes Set a plain upsert and GetActive a point read.
const activeLicenseID = "active"

// LicenseStore implements store.LicenseStore using PostgreSQL. The
// installed self-hosted license lives as a single row in the licenses
// table (migration 024). Set replaces it (upsert on the fixed id),
// GetActive reads it, Delete clears it.
type LicenseStore struct {
	db *sql.DB
}

// NewLicenseStore creates a PostgreSQL-backed license store.
func NewLicenseStore(db *sql.DB) *LicenseStore {
	return &LicenseStore{db: db}
}

func (s *LicenseStore) GetActive(ctx context.Context) (*model.License, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, raw_token, plan, seats, features, customer,
		        issued_at, expires_at, installed_at, installed_by_sub, installed_by_email
		   FROM licenses WHERE id = $1`, activeLicenseID)
	l, err := scanLicense(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("active license: %w", store.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return l, nil
}

func (s *LicenseStore) Set(ctx context.Context, l *model.License) error {
	// Single active license: always key on the fixed sentinel id so a
	// second Set replaces the first rather than inserting a second row.
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO licenses
		   (id, raw_token, plan, seats, features, customer, issued_at, expires_at,
		    installed_at, installed_by_sub, installed_by_email)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,COALESCE($9, NOW()),$10,$11)
		 ON CONFLICT (id) DO UPDATE SET
		    raw_token          = EXCLUDED.raw_token,
		    plan               = EXCLUDED.plan,
		    seats              = EXCLUDED.seats,
		    features           = EXCLUDED.features,
		    customer           = EXCLUDED.customer,
		    issued_at          = EXCLUDED.issued_at,
		    expires_at         = EXCLUDED.expires_at,
		    installed_at       = EXCLUDED.installed_at,
		    installed_by_sub   = EXCLUDED.installed_by_sub,
		    installed_by_email = EXCLUDED.installed_by_email
		 RETURNING installed_at`,
		activeLicenseID, l.RawToken, l.Plan, l.Seats, pq.Array(l.Features),
		l.Customer, l.IssuedAt, nullTime(l.ExpiresAt), nullTime(zeroToNil(l.InstalledAt)),
		l.InstalledBySub, l.InstalledByEmail).
		Scan(&l.InstalledAt)
	if err != nil {
		return fmt.Errorf("setting license: %w", err)
	}
	// Normalise the in-memory id to the stored sentinel so callers that
	// reuse the pointer see what persisted (mirrors memory keeping its
	// own copy authoritative).
	l.ID = activeLicenseID
	return nil
}

func (s *LicenseStore) Delete(ctx context.Context) error {
	// Idempotent clear: deleting when none is installed is a no-op (no
	// error), matching the memory impl and "remove license" intent.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM licenses WHERE id = $1`, activeLicenseID); err != nil {
		return fmt.Errorf("deleting license: %w", err)
	}
	return nil
}

// zeroToNil returns a pointer to t, or nil when t is the zero time, so a
// not-yet-stamped InstalledAt becomes a SQL NULL and the COALESCE in Set
// fills it with NOW() — mirroring the memory impl's "stamp when zero".
func zeroToNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func scanLicense(row scannable) (*model.License, error) {
	l := &model.License{}
	var expiresAt sql.NullTime
	var features pq.StringArray
	if err := row.Scan(&l.ID, &l.RawToken, &l.Plan, &l.Seats, &features, &l.Customer,
		&l.IssuedAt, &expiresAt, &l.InstalledAt, &l.InstalledBySub, &l.InstalledByEmail); err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		e := expiresAt.Time
		l.ExpiresAt = &e
	}
	if features != nil {
		l.Features = []string(features)
	}
	return l, nil
}
