package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"time"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// auditQueryDefaultLimit caps Query when the caller passes <= 0.
const auditQueryDefaultLimit = 50

// AuditEventStore implements store.AuditEventStore using PostgreSQL.
type AuditEventStore struct {
	db *sql.DB
}

// NewAuditEventStore creates a PostgreSQL-backed audit-trail store.
func NewAuditEventStore(db *sql.DB) *AuditEventStore {
	return &AuditEventStore{db: db}
}

func (s *AuditEventStore) Insert(ctx context.Context, e *model.AuditEvent) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO audit_events
		   (time, user_sub, user_email, method, path, status, latency_ms, client_ip)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id`,
		e.Time, e.UserSub, e.UserEmail, e.Method, e.Path, e.Status, e.LatencyMS, e.ClientIP).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("inserting audit event: %w", err)
	}
	return nil
}

func (s *AuditEventStore) Query(ctx context.Context, q store.AuditQuery) ([]*model.AuditEvent, error) {
	where := []string{"TRUE"}
	args := []interface{}{}
	add := func(cond string, v interface{}) {
		args = append(args, v)
		where = append(where, cond+"$"+strconv.Itoa(len(args)))
	}
	if q.From != nil {
		add("time >= ", *q.From)
	}
	if q.To != nil {
		add("time <= ", *q.To)
	}
	if q.UserSub != "" {
		add("user_sub = ", q.UserSub)
	}
	if q.Method != "" {
		add("method = ", q.Method)
	}
	if q.PathPrefix != "" {
		// Literal prefix match: escape LIKE metacharacters so a filter
		// like "/api/v1/apps_%" can't widen the match.
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q.PathPrefix)
		add("path LIKE ", esc+"%")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = auditQueryDefaultLimit
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)

	// where only ever contains parameter placeholders built above —
	// no user input is concatenated into the SQL text.
	query := `SELECT id, time, user_sub, user_email, method, path, status, latency_ms, client_ip
	   FROM audit_events WHERE ` + strings.Join(where, " AND ") + `
	  ORDER BY time DESC
	  LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying audit events: %w", err)
	}
	defer rows.Close()

	var out []*model.AuditEvent
	for rows.Next() {
		e := &model.AuditEvent{}
		if err := rows.Scan(&e.ID, &e.Time, &e.UserSub, &e.UserEmail, &e.Method,
			&e.Path, &e.Status, &e.LatencyMS, &e.ClientIP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *AuditEventStore) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE time < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("audit retention sweep: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
