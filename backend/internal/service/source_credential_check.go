package service

import (
	"context"
	"strings"
	"sync"

	"github.com/santapong/cooker/internal/source/connection"
	"github.com/santapong/cooker/internal/source/gittransport"
)

// CheckCredentialReplacement checks repository metadata with the replacement
// token on the connection's approved origin. It does not clone, list refs,
// mutate provider resources or certify future checkout permissions.
func (s *SourceAccess) CheckCredentialReplacement(ctx context.Context, candidate *connection.Connection) error {
	if s == nil || s.Connections == nil || candidate == nil || candidate.AuthMode != "token" || connection.Validate(candidate) != nil {
		return connection.ErrCredentialCheck
	}
	c := *candidate
	c.Repositories = append([]string(nil), candidate.Repositories...)
	// Checking a disabled connection must not enable the saved record.
	c.Enabled = true
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	work := make(chan string, len(c.Repositories))
	for _, repo := range c.Repositories {
		work <- repo
	}
	close(work)
	var mu sync.Mutex
	failed := false
	var wg sync.WaitGroup
	for i := 0; i < min(4, len(c.Repositories)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range work {
				if ctx.Err() != nil {
					return
				}
				id, err := s.Identity(ctx, &c, repo)
				if err != nil || !gittransport.ValidID(id.RepositoryID) || !strings.EqualFold(id.Repository, repo) {
					mu.Lock()
					failed = true
					mu.Unlock()
					cancel()
					return
				}
			}
		}()
	}
	wg.Wait()
	if failed || ctx.Err() != nil {
		return connection.ErrCredentialCheck
	}
	return nil
}
