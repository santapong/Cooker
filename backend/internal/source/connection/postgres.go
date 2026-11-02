package connection

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type Postgres struct{ DB *sql.DB }

func (s *Postgres) List(ctx context.Context) ([]*Connection, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,configuration,credential,version,created_at,updated_at FROM source_connections ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Connection{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Postgres) Get(ctx context.Context, id string) (*Connection, error) {
	c, err := scan(s.DB.QueryRowContext(ctx, `SELECT id,configuration,credential,version,created_at,updated_at FROM source_connections WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}
func (s *Postgres) Create(ctx context.Context, c *Connection) error {
	config, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO source_connections (id,configuration,credential,version,created_at,updated_at) VALUES ($1,$2,$3,1,$4,$5)`, c.ID, config, c.Credential, c.CreatedAt, c.UpdatedAt)
	return err
}
func (s *Postgres) Update(ctx context.Context, c *Connection) error {
	config, err := json.Marshal(c)
	if err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE source_connections SET configuration=$2,credential=$3,version=version+1,updated_at=$4 WHERE id=$1 AND version=$5`, c.ID, config, c.Credential, c.UpdatedAt, c.Version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	c.Version++
	return nil
}
func scan(row interface{ Scan(...any) error }) (*Connection, error) {
	c := &Connection{}
	var config []byte
	if err := row.Scan(&c.ID, &config, &c.Credential, &c.Version, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	var value Connection
	if err := json.Unmarshal(config, &value); err != nil {
		return nil, err
	}
	value.ID, value.Credential, value.Version, value.CreatedAt, value.UpdatedAt = c.ID, c.Credential, c.Version, c.CreatedAt, c.UpdatedAt
	value.HasWebhookSecret = len(c.Credential) > 0
	return &value, nil
}
