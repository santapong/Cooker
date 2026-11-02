package logstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Postgres stores only already-redacted entries. Stream row locks serialize
// cursor allocation and trimming across processes. The caller owns db.
type Postgres struct {
	db     *sql.DB
	limits Limits
}

func NewPostgres(db *sql.DB, limits Limits) *Postgres {
	return &Postgres{db: db, limits: limits.defaults()}
}

func (p *Postgres) Append(ctx context.Context, runID, stageID string, e Entry) error {
	_, err := p.append(ctx, runID, stageID, e, false)
	return err
}

func (p *Postgres) AppendNext(ctx context.Context, runID, stageID string, e Entry) (Entry, error) {
	return p.append(ctx, runID, stageID, e, true)
}

func boundedLine(line string, cap int) string {
	line = strings.ToValidUTF8(strings.ReplaceAll(line, "\x00", "�"), "�")
	if len(line) <= cap {
		return line
	}
	// Truncate before converting to valid UTF-8 to avoid invalid trailing bytes.
	return strings.ToValidUTF8(line[:cap], "")
}

func (p *Postgres) append(ctx context.Context, runID, stageID string, e Entry, next bool) (Entry, error) {
	if runID == "" || stageID == "" || (!next && e.Seq < 1) {
		return Entry{}, fmt.Errorf("logstore: invalid stream or cursor")
	}
	e.Line = boundedLine(e.Line, p.limits.MaxBytesPerStage)
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = tx.Rollback() }() // Commit/operation errors determine the result.
	_, err = tx.ExecContext(ctx, `INSERT INTO stage_log_streams(run_id,stage_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, runID, stageID)
	if err != nil {
		return Entry{}, err
	}
	var last int
	if err = tx.QueryRowContext(ctx, `SELECT last_seq FROM stage_log_streams WHERE run_id=$1 AND stage_id=$2 FOR UPDATE`, runID, stageID).Scan(&last); err != nil {
		return Entry{}, err
	}
	if next {
		e.Seq = last + 1
	} else if e.Seq <= last {
		return Entry{}, fmt.Errorf("logstore: cursor must advance")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO stage_log_entries(run_id,stage_id,seq,timestamp,line) VALUES($1,$2,$3,$4,$5)`, runID, stageID, e.Seq, e.TS, e.Line); err != nil {
		return Entry{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE stage_log_streams SET last_seq=$3,updated_at=NOW() WHERE run_id=$1 AND stage_id=$2`, runID, stageID, e.Seq); err != nil {
		return Entry{}, err
	}
	// Keep the newest entries that fit the byte budget. Counting an empty line
	// as one byte also bounds pathological output made entirely of newlines.
	_, err = tx.ExecContext(ctx, `DELETE FROM stage_log_entries WHERE run_id=$1 AND stage_id=$2 AND seq IN (
	 SELECT seq FROM (SELECT seq,SUM(GREATEST(OCTET_LENGTH(line),1)) OVER (ORDER BY seq DESC) AS bytes,ROW_NUMBER() OVER (ORDER BY seq DESC) AS position FROM stage_log_entries WHERE run_id=$1 AND stage_id=$2) tail WHERE bytes>$3 OR position>$4
	)`, runID, stageID, p.limits.MaxBytesPerStage, maxEntriesPerStage)
	if err != nil {
		return Entry{}, err
	}
	if err = tx.Commit(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

func (p *Postgres) Read(ctx context.Context, runID, stageID string, since int) ([]Entry, error) {
	entries, _, err := p.History(ctx, runID, stageID, since)
	return entries, err
}

func (p *Postgres) History(ctx context.Context, runID, stageID string, since int) ([]Entry, int, error) {
	// One statement gives a coherent tail/cursor snapshot even during appends.
	rows, err := p.db.QueryContext(ctx, `SELECT s.last_seq,e.seq,e.timestamp,e.line FROM stage_log_streams s LEFT JOIN stage_log_entries e ON e.run_id=s.run_id AND e.stage_id=s.stage_id AND e.seq>$3 AND e.timestamp >= $4 WHERE s.run_id=$1 AND s.stage_id=$2 ORDER BY e.seq`, runID, stageID, since, time.Now().Add(-p.limits.Retention))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var entries []Entry
	var latest int
	for rows.Next() {
		var seq sql.NullInt64
		var ts sql.NullTime
		var line sql.NullString
		if err = rows.Scan(&latest, &seq, &ts, &line); err != nil {
			return nil, 0, err
		}
		if seq.Valid {
			entries = append(entries, Entry{Seq: int(seq.Int64), TS: ts.Time, Line: line.String})
		}
	}
	return entries, latest, rows.Err()
}

// Prune enforces the installation-wide stream count and retention window.
// Invoke periodically and at boot. Per-stream bytes are bounded on every append.
func (p *Postgres) Prune(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM stage_log_streams WHERE (run_id,stage_id) IN (SELECT run_id,stage_id FROM stage_log_streams WHERE updated_at < $1 UNION SELECT run_id,stage_id FROM (SELECT run_id,stage_id FROM stage_log_streams ORDER BY updated_at DESC OFFSET $2) old)`, time.Now().Add(-p.limits.Retention), p.limits.MaxStreams)
	return err
}
