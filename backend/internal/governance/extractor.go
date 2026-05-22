package governance

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/store"
)

// AppDeployExtractor returns a GovernanceResourceExtractor-shaped function for
// POST /apps/:id/deploy. It looks up the App by its URL parameter, then
// resolves the linked Environment to derive the env name. The returned
// service name is the App name (lower-cased); the env name is the
// Environment name (lower-cased). When the App has no Environment, ok=false
// so the middleware permits the request — there's nothing to gate against.
//
// This function lives here (not in the auth package) so it can take a *Store
// dependency without auth importing store. The middleware itself sits in auth
// and takes the extractor as an opaque function value.
func AppDeployExtractor(st *store.Store) func(c *gin.Context) (string, string, bool, error) {
	return func(c *gin.Context) (string, string, bool, error) {
		id := c.Param("id")
		if id == "" {
			return "", "", false, nil
		}
		app, err := st.Apps.Get(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				// Let the handler return 404; nothing to gate.
				return "", "", false, nil
			}
			return "", "", false, err
		}
		if app == nil || app.EnvironmentID == "" {
			// Pre-Phase-3 Apps without an Environment link can't be gated;
			// fall back to letting the existing RBAC + audit middleware
			// decide. The operator will see these in the slog warnings.
			return "", "", false, nil
		}
		env, err := st.Environments.Get(c.Request.Context(), app.EnvironmentID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return "", "", false, nil
			}
			return "", "", false, err
		}
		if env == nil {
			return "", "", false, nil
		}
		return strings.ToLower(app.Name), strings.ToLower(env.Name), true, nil
	}
}
