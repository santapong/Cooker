package server

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/cooker-ci/cooker/internal/observability"
)

// redisWSBroadcastChannel is the single Redis pub/sub channel that
// every Cooker replica publishes to and subscribes to. The per-client
// fan-out (which channel name a browser is listening on) stays inside
// each replica's local clients map.
const redisWSBroadcastChannel = "cooker:ws:broadcast"

// HubBackend is the swap point for the WebSocket fan-out layer.
// Memory keeps the historical in-process channel; Redis pub/sub
// delivers broadcasts across replicas. Per-client subscription state
// stays local to each replica regardless.
type HubBackend interface {
	// Publish enqueues a broadcast for delivery. Implementations may
	// drop / coalesce on backpressure but must not block forever.
	Publish(msg BroadcastMessage) error
	// Subscribe returns the channel the hub's Run loop reads from.
	// The same channel is returned on every call.
	Subscribe() <-chan BroadcastMessage
	// Close releases backend resources. Safe to call multiple times.
	Close() error
}

// memoryHubBackend is the default in-process channel-based backend.
// Behaviour matches the historical Hub.broadcast usage.
type memoryHubBackend struct {
	ch chan BroadcastMessage
}

func newMemoryHubBackend() *memoryHubBackend {
	return &memoryHubBackend{ch: make(chan BroadcastMessage, 256)}
}

func (b *memoryHubBackend) Publish(msg BroadcastMessage) error {
	b.ch <- msg
	return nil
}

func (b *memoryHubBackend) Subscribe() <-chan BroadcastMessage { return b.ch }

func (b *memoryHubBackend) Close() error { return nil }

// redisHubBackend uses Redis pub/sub. Each replica's hub publishes
// every Broadcast through Redis and subscribes to the shared topic so
// inbound messages arrive on the .ch returned by Subscribe.
type redisHubBackend struct {
	client *redis.Client
	pubsub *redis.PubSub
	ch     chan BroadcastMessage
	cancel context.CancelFunc
}

func newRedisHubBackend(ctx context.Context, client *redis.Client) (*redisHubBackend, error) {
	subCtx, cancel := context.WithCancel(ctx)
	ps := client.Subscribe(subCtx, redisWSBroadcastChannel)
	if _, err := ps.Receive(subCtx); err != nil {
		cancel()
		_ = ps.Close()
		return nil, err
	}
	b := &redisHubBackend{
		client: client,
		pubsub: ps,
		ch:     make(chan BroadcastMessage, 256),
		cancel: cancel,
	}
	go b.consume(subCtx, ps.Channel())
	return b, nil
}

func (b *redisHubBackend) Publish(msg BroadcastMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if err := b.client.Publish(context.Background(), redisWSBroadcastChannel, payload).Err(); err != nil {
		observability.IncRedisConnectionError()
		return err
	}
	return nil
}

func (b *redisHubBackend) Subscribe() <-chan BroadcastMessage { return b.ch }

func (b *redisHubBackend) Close() error {
	if b.cancel != nil {
		b.cancel()
	}
	return b.pubsub.Close()
}

func (b *redisHubBackend) consume(ctx context.Context, source <-chan *redis.Message) {
	for {
		select {
		case <-ctx.Done():
			close(b.ch)
			return
		case raw, ok := <-source:
			if !ok {
				close(b.ch)
				return
			}
			var msg BroadcastMessage
			if err := json.Unmarshal([]byte(raw.Payload), &msg); err != nil {
				slog.Warn("ws redis backend: unmarshal", "err", err)
				continue
			}
			select {
			case b.ch <- msg:
			default:
				slog.Warn("ws redis backend: local channel full; dropping broadcast", "channel", msg.Channel)
			}
		}
	}
}
