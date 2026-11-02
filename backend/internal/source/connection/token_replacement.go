package connection

import (
	"context"
	"errors"

	"github.com/santapong/cooker/internal/source/gittransport"
)

var ErrTokenReplacementInput = errors.New("replacement requires a token connection, its current positive version and a nonempty token")

// ReplaceToken changes only the repository token and its explicit Git username.
// The saved webhook secret, origin, allowlist, enabled state and creation
// identity are retained. UpdateWithCredentials performs the read preflight and
// the final version-fenced write; a concurrent edit cannot be overwritten.
func (s *Service) ReplaceToken(ctx context.Context, id string, version int, token string, username *string) (*Connection, error) {
	if s == nil || s.Store == nil || s.Codec == nil {
		return nil, ErrCredentialCheckUnavailable
	}
	if ctx.Err() != nil {
		return nil, ErrCredentialCheck
	}
	if version <= 0 || token == "" {
		return nil, ErrTokenReplacementInput
	}
	c, err := s.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Version != version {
		return nil, ErrConflict
	}
	if c.AuthMode != "token" {
		return nil, ErrTokenReplacementInput
	}
	if username != nil {
		c.Username = *username
	}
	if Validate(c) != nil || gittransport.ValidateCredentials(gittransport.Credentials{Username: c.Username, Token: token}) != nil {
		return nil, ErrTokenReplacementInput
	}
	if err := s.UpdateWithCredentials(ctx, c, CredentialsPatch{Token: &token}); err != nil {
		return nil, err
	}
	return c, nil
}
