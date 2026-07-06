package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisTicketStore is the multi-replica equivalent of wsTicketStore.
// Tickets are kept in Redis with a TTL; Consume uses GETDEL (atomic
// since Redis 6.2) so a single ticket can never be redeemed twice
// even when two cooker replicas race on the same upgrade. The grant
// (subject, roles, path) is JSON-serialised into the value so role +
// resource scope survives across replicas (CR-6).
type redisTicketStore struct {
	client *redis.Client
	ttl    time.Duration
	prefix string
}

func newRedisTicketStore(client *redis.Client, ttl time.Duration) *redisTicketStore {
	return &redisTicketStore{client: client, ttl: ttl, prefix: "cooker:ws-ticket:"}
}

func (s *redisTicketStore) Issue(grant ticketGrant) (string, time.Time, error) {
	if s.ttl <= 0 {
		return "", time.Time{}, errors.New("ws-ticket: non-positive ttl")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	tok := base64.RawURLEncoding.EncodeToString(buf)
	payload, err := json.Marshal(grant)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(s.ttl)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.client.Set(ctx, s.prefix+tok, payload, s.ttl).Err(); err != nil {
		return "", time.Time{}, err
	}
	return tok, expires, nil
}

func (s *redisTicketStore) Consume(tok string) (ticketGrant, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	payload, err := s.client.GetDel(ctx, s.prefix+tok).Result()
	if err != nil || payload == "" {
		return ticketGrant{}, false
	}
	var grant ticketGrant
	if err := json.Unmarshal([]byte(payload), &grant); err != nil {
		return ticketGrant{}, false
	}
	return grant, true
}
