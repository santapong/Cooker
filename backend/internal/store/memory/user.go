package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

// users is an in-memory store.UserStore. Email keys are case-insensitive
// (lowercased on the way in) so callers don't have to normalise.
type users struct {
	mu      sync.RWMutex
	byID    map[string]*model.User
	byEmail map[string]string // lowercased email → id
}

func (s *users) GetByEmail(_ context.Context, email string) (*model.User, error) {
	key := strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byEmail[key]
	if !ok {
		return nil, fmt.Errorf("user %s: %w", email, store.ErrNotFound)
	}
	return s.byID[id], nil
}

func (s *users) GetByID(_ context.Context, id string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, fmt.Errorf("user %s: %w", id, store.ErrNotFound)
	}
	return u, nil
}

func (s *users) Create(_ context.Context, u *model.User) error {
	key := strings.ToLower(strings.TrimSpace(u.Email))
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byEmail[key]; exists {
		return fmt.Errorf("user %s: already exists", u.Email)
	}
	s.byID[u.ID] = u
	s.byEmail[key] = u.ID
	return nil
}

func (s *users) Update(_ context.Context, u *model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.byID[u.ID]
	if !ok {
		return fmt.Errorf("user %s: %w", u.ID, store.ErrNotFound)
	}
	if !strings.EqualFold(old.Email, u.Email) {
		delete(s.byEmail, strings.ToLower(strings.TrimSpace(old.Email)))
		s.byEmail[strings.ToLower(strings.TrimSpace(u.Email))] = u.ID
	}
	s.byID[u.ID] = u
	return nil
}

func (s *users) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID), nil
}
