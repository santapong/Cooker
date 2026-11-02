package vercel

import (
	"context"
	"net/url"

	"github.com/lib/pq"
)

// VerifiedForDeployments returns durable verified receipts for exact deployment
// IDs. Unlike the recent operation list, unrelated newer reviews cannot evict
// source evidence. Callers must also bind account, scope and project identity.
func (p *Publisher) VerifiedForDeployments(ctx context.Context, ids []string) ([]*Publication, error) {
	out := []*Publication{}
	if len(ids) == 0 {
		return out, nil
	}
	if p == nil || p.DB == nil {
		return nil, ErrUnavailable
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if !validProviderID(id) {
			return nil, ErrInvalid
		}
		if !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT receipt,actor,status,created_at,updated_at FROM vercel_publications WHERE status='succeeded' AND receipt->>'deploymentId'=ANY($1::text[]) ORDER BY created_at DESC,id DESC`, pq.Array(unique))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		op, err := readPublication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

// SafeDisplayURL accepts only credential-free public HTTPS display links.
// dashboard additionally restricts links to the Vercel dashboard origin.
// These links are never provider request targets.
func SafeDisplayURL(raw string, dashboard bool) string {
	safe := deploymentURL(raw)
	if safe == "" {
		return ""
	}
	if dashboard {
		u, err := url.Parse(safe)
		if err != nil || (u.Hostname() != "vercel.com" && u.Hostname() != "www.vercel.com") {
			return ""
		}
	}
	return safe
}
