package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

func (t *TriggerStore) WithNativeTriggerAdmission(ctx context.Context, id string, work func(context.Context) error) error {
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "\x00\r\n") || work == nil {
		return fmt.Errorf("native admission identity is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	hash := sha256.Sum256([]byte(id))
	key := int32(binary.BigEndian.Uint32(hash[:4]))
	const namespace = int32(0x43475433)
	for {
		conn, err := t.db.Conn(ctx)
		if err != nil {
			return err
		}
		var locked bool
		err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1,$2)`, namespace, key).Scan(&locked)
		if err != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
			conn.Close()
			return err
		}
		if locked {
			defer func() {
				release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				defer cancel()
				var released bool
				if err := conn.QueryRowContext(release, `SELECT pg_advisory_unlock($1,$2)`, namespace, key).Scan(&released); err != nil || !released {
					conn.Raw(func(any) error { return driver.ErrBadConn })
				}
				conn.Close()
			}()
			return work(ctx)
		}
		conn.Close() // Never consume pool capacity while waiting for another owner.
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
