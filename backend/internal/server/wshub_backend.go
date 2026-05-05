package server

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"log/slog"
	mrand "math/rand"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cooker-ci/cooker/internal/observability"
)

// secureJitter returns a uniformly-random duration in [0, d/2]. The
// jitter is for backoff-spread, not security, but using crypto/rand
// avoids the global math/rand source's lock contention on hot paths
// and keeps the project's "math/rand only for non-essential
// randomness" convention consistent.
func secureJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	max := int64(d/2) + 1
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to math/rand if the crypto source is somehow
		// unavailable. This branch is unreachable in practice on
		// Linux but we never want backoff to panic.
		return time.Duration(mrand.Int63n(max))
	}
	n := int64(binary.BigEndian.Uint64(b[:]) & ((1 << 62) - 1))
	return time.Duration(n % max)
}

// redisWSBroadcastChannel is the single Redis pub/sub channel that
// every Cooker replica publishes to and subscribes to. The per-client
// fan-out (which channel name a browser is listening on) stays inside
// each replica's local clients map.
const redisWSBroadcastChannel = "cooker:ws:broadcast"

// Backoff parameters for the Redis subscriber. Tuned to recover quickly
// from a brief blip without hammering a flapping Redis. Receive timeout
// caps how long a half-open TCP connection can wedge the resubscribe
// loop past TCP keepalive defaults.
const (
	redisHubInitialBackoff = 500 * time.Millisecond
	redisHubMaxBackoff     = 30 * time.Second
	redisHubReceiveTimeout = 5 * time.Second
)

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
//
// The subscriber loop in consume() survives Redis blips: the channel
// returned by Subscribe() only closes when Close() is called. Messages
// published during a disconnect window are lost (Redis pub/sub has no
// replay) — clients refetch state via WS reconnect.
type redisHubBackend struct {
	client *redis.Client
	ch     chan BroadcastMessage
	ctx    context.Context
	cancel context.CancelFunc
}

func newRedisHubBackend(parent context.Context, client *redis.Client) (*redisHubBackend, error) {
	ctx, cancel := context.WithCancel(parent)
	// Boot-time connectivity check — fail fast if Redis is unreachable
	// at startup. Once consume() takes over, transient disconnects are
	// handled by the resubscribe loop instead.
	ps := client.Subscribe(ctx, redisWSBroadcastChannel)
	if _, err := ps.Receive(ctx); err != nil {
		cancel()
		_ = ps.Close()
		return nil, err
	}
	_ = ps.Close()
	b := &redisHubBackend{
		client: client,
		ch:     make(chan BroadcastMessage, 256),
		ctx:    ctx,
		cancel: cancel,
	}
	go b.consume()
	return b, nil
}

func (b *redisHubBackend) Publish(msg BroadcastMessage) error {
	payload, err := encodeBroadcast(msg)
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
	return nil
}

// consume keeps a Redis pub/sub subscription open across transient
// disconnects. When the underlying channel closes (Redis flap, network
// blip), it waits with jittered exponential backoff and resubscribes.
// Only context cancellation closes b.ch, so readers see a stable
// stream across blips. Each iteration owns its *redis.PubSub; the
// previous one is closed before re-subscribing so we don't leak
// connections or goroutines.
func (b *redisHubBackend) consume() {
	defer close(b.ch)
	delay := redisHubInitialBackoff
	for {
		if b.ctx.Err() != nil {
			return
		}
		ps := b.client.Subscribe(b.ctx, redisWSBroadcastChannel)
		recvCtx, recvCancel := context.WithTimeout(b.ctx, redisHubReceiveTimeout)
		_, err := ps.Receive(recvCtx)
		recvCancel()
		if err != nil {
			_ = ps.Close()
			if b.ctx.Err() != nil {
				return
			}
			observability.IncRedisConnectionError()
			slog.Warn("ws redis backend: subscribe failed; backing off", "delay", delay.String(), "err", err)
			if !sleepJitter(b.ctx, delay) {
				return
			}
			delay = nextBackoff(delay, redisHubMaxBackoff)
			continue
		}
		// Resubscribe succeeded — reset the next failure's delay.
		delay = redisHubInitialBackoff
		b.drain(ps.Channel())
		_ = ps.Close()
		if b.ctx.Err() != nil {
			return
		}
		observability.IncRedisConnectionError()
		slog.Warn("ws redis backend: subscription closed; reconnecting", "delay", delay.String())
		if !sleepJitter(b.ctx, delay) {
			return
		}
		delay = nextBackoff(delay, redisHubMaxBackoff)
	}
}

// drain reads from a single subscription's channel until it closes
// (Redis flap) or the parent context is cancelled. Returns either way.
// b.ch is intentionally NOT closed here — only consume's defer does
// that, and only on context cancellation.
func (b *redisHubBackend) drain(source <-chan *redis.Message) {
	for {
		select {
		case <-b.ctx.Done():
			return
		case raw, ok := <-source:
			if !ok {
				return
			}
			msg, err := decodeBroadcast([]byte(raw.Payload))
			if err != nil {
				slog.Warn("ws redis backend: decode", "err", err)
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

// sleepJitter sleeps for d plus 0..d/2 jitter, returning false if ctx
// fires during the sleep. Uses NewTimer so the timer is reclaimed when
// ctx wins the race.
func sleepJitter(ctx context.Context, d time.Duration) bool {
	jittered := d + secureJitter(d)
	t := time.NewTimer(jittered)
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		t.Stop()
		return false
	}
}

// nextBackoff doubles d up to max.
func nextBackoff(d, max time.Duration) time.Duration {
	d *= 2
	if d > max {
		d = max
	}
	return d
}

// Wire format for cross-replica broadcast messages over Redis pub/sub:
//
//	[channel-len: uint16 big-endian][channel: bytes][data: bytes]
//
// The format is internal — only the redisHubBackend reads or writes it,
// and Cooker pushes the same wire format from every replica. A rolling
// upgrade across replicas with mixed encodings will see decode failures
// (logged at warn) for the duration of the upgrade window; messages
// resume cleanly once all replicas are on the same version. Avoid
// mixing versions over long periods.
//
// Switched from json.Marshal in P0.5: typical broadcast carries an
// app-run channel string (~46 bytes) and a small log-line data
// payload, where JSON framing previously added ~74 bytes per message.
// Binary framing is 2 bytes plus the channel and data.
const broadcastChannelLenBytes = 2

func encodeBroadcast(msg BroadcastMessage) ([]byte, error) {
	if len(msg.Channel) > int(^uint16(0)) {
		return nil, errors.New("ws redis backend: channel name exceeds 65535 bytes")
	}
	out := make([]byte, broadcastChannelLenBytes+len(msg.Channel)+len(msg.Data))
	binary.BigEndian.PutUint16(out[:broadcastChannelLenBytes], uint16(len(msg.Channel)))
	copy(out[broadcastChannelLenBytes:], msg.Channel)
	copy(out[broadcastChannelLenBytes+len(msg.Channel):], msg.Data)
	return out, nil
}

func decodeBroadcast(payload []byte) (BroadcastMessage, error) {
	if len(payload) < broadcastChannelLenBytes {
		return BroadcastMessage{}, errors.New("ws redis backend: payload too short for channel length prefix")
	}
	chLen := int(binary.BigEndian.Uint16(payload[:broadcastChannelLenBytes]))
	if len(payload) < broadcastChannelLenBytes+chLen {
		return BroadcastMessage{}, errors.New("ws redis backend: payload truncated mid-channel")
	}
	start := broadcastChannelLenBytes
	end := start + chLen
	return BroadcastMessage{
		Channel: string(payload[start:end]),
		Data:    append([]byte(nil), payload[end:]...),
	}, nil
}
