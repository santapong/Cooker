package connection

import (
	"context"
	"sync"
)

type Memory struct {
	mu    sync.Mutex
	items map[string]*Connection
}

func NewMemory() *Memory { return &Memory{items: map[string]*Connection{}} }
func copyConnection(c *Connection) *Connection {
	cp := *c
	cp.Credential = append([]byte(nil), c.Credential...)
	cp.Repositories = append([]string(nil), c.Repositories...)
	return &cp
}
func (s *Memory) List(context.Context) ([]*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*Connection{}
	for _, c := range s.items {
		out = append(out, copyConnection(c))
	}
	return out, nil
}
func (s *Memory) Get(_ context.Context, id string) (*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return copyConnection(c), nil
}
func (s *Memory) Create(_ context.Context, c *Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items[c.ID] != nil {
		return ErrConflict
	}
	s.items[c.ID] = copyConnection(c)
	return nil
}
func (s *Memory) Update(_ context.Context, c *Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.items[c.ID]
	if previous == nil || previous.Version != c.Version {
		return ErrConflict
	}
	c.Version++
	s.items[c.ID] = copyConnection(c)
	return nil
}
