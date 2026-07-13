package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/service"
	"github.com/santapong/cooker/internal/validate"
)

// hostRequest is the wire shape for POST/PUT /hosts. It mirrors
// model.Host but adds the write-only sshPrivateKeyPem field that
// is never echoed back in any GET response (the model strips it
// from JSON via Redact).
type hostRequest struct {
	model.Host
	// SSHPrivateKeyPem is the PEM body the operator pastes into the
	// "Add host" form. On Create it must be supplied for SSH hosts;
	// on Update it is optional (empty = keep the existing key).
	// Never returned in any response.
	SSHPrivateKeyPem string `json:"sshPrivateKeyPem,omitempty"`
}

func validateHostInput(h *model.Host) error {
	if err := validate.Name("name", h.Name); err != nil {
		return err
	}
	if err := validate.HostKind(h.Kind); err != nil {
		return err
	}
	// Reachability is optional on Create (defaults to direct).
	if h.Reachability != "" {
		if err := validate.HostReachability(h.Reachability); err != nil {
			return err
		}
	}
	if h.Kind == model.HostKindSSHDocker {
		if h.SSHEndpoint == "" {
			return errors.New("sshEndpoint is required for ssh-docker hosts")
		}
		if h.SSHUser == "" {
			return errors.New("sshUser is required for ssh-docker hosts")
		}
	}
	return nil
}

func (h *Handler) ListHosts(c *gin.Context) {
	limit := intQuery(c, "limit", listDefaultLimit, 1, listMaxLimit)
	offset := intQuery(c, "offset", 0, 0, 1<<30)
	hosts, err := h.Store.Hosts.List(c.Request.Context(), limit, offset)
	if abortStoreErr(c, err, "hosts not found") {
		return
	}
	out := make([]*model.HostResponse, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, host.Redact())
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) GetHost(c *gin.Context) {
	host, err := h.Store.Hosts.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "host not found") {
		return
	}
	c.JSON(http.StatusOK, host.Redact())
}

func (h *Handler) CreateHost(c *gin.Context) {
	var req hostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	host := req.Host
	if err := validateHostInput(&host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if host.Reachability == "" {
		host.Reachability = model.HostDirect
	}
	host.ID = uuid.New().String()
	now := time.Now()
	host.CreatedAt, host.UpdatedAt = now, now

	if host.Kind == model.HostKindSSHDocker {
		if host.SSHPort == 0 {
			host.SSHPort = 22
		}
		// Default strict-host-key ON. The model's bool zero-value is
		// false; we treat "absent in JSON" as "operator didn't say,
		// pick the secure default". The frontend always sends the
		// explicit value, but defensive coding here protects against
		// API clients that omit the field.
		if !host.SSHStrictHostKey && req.SSHPrivateKeyPem == "" && host.SSHKnownHostKey == "" {
			// Likely-default zero value; flip on.
			host.SSHStrictHostKey = true
		}
		if req.SSHPrivateKeyPem == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sshPrivateKeyPem is required for ssh-docker hosts"})
			return
		}
		if h.Hosts == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "host service not configured (set COOKER_SECRETS_BACKEND)"})
			return
		}
		if err := h.Hosts.CreateHost(c.Request.Context(), &host, []byte(req.SSHPrivateKeyPem)); err != nil {
			handleHostServiceError(c, err)
			return
		}
		c.JSON(http.StatusCreated, host.Redact())
		return
	}

	if err := h.Store.Hosts.Create(c.Request.Context(), &host); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, host.Redact())
}

func (h *Handler) UpdateHost(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.Store.Hosts.Get(c.Request.Context(), id)
	if abortStoreErr(c, err, "host not found") {
		return
	}
	var req hostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	host := req.Host
	if err := validateHostInput(&host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	host.ID = id
	host.CreatedAt = existing.CreatedAt
	host.UpdatedAt = time.Now()

	if host.Kind == model.HostKindSSHDocker {
		if host.SSHPort == 0 {
			host.SSHPort = 22
		}
		if h.Hosts == nil && req.SSHPrivateKeyPem != "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "host service not configured (set COOKER_SECRETS_BACKEND)"})
			return
		}
		if h.Hosts != nil {
			if err := h.Hosts.UpdateHost(c.Request.Context(), existing, &host, []byte(req.SSHPrivateKeyPem)); err != nil {
				handleHostServiceError(c, err)
				return
			}
			c.JSON(http.StatusOK, host.Redact())
			return
		}
	}

	// Non-SSH hosts: preserve any pre-existing SSH ref/known-host on
	// the row (handler should never silently drop a field it doesn't
	// understand from a payload that omits it).
	host.SSHPrivateKeyRef = existing.SSHPrivateKeyRef
	host.SSHKnownHostKey = existing.SSHKnownHostKey
	if err := h.Store.Hosts.Update(c.Request.Context(), &host); err != nil {
		if abortStoreErr(c, err, "host not found") {
			return
		}
	}
	c.JSON(http.StatusOK, host.Redact())
}

func (h *Handler) DeleteHost(c *gin.Context) {
	if h.Hosts != nil {
		if err := h.Hosts.DeleteHost(c.Request.Context(), c.Param("id")); err != nil {
			if abortStoreErr(c, err, "host not found") {
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"message": "deleted"})
		return
	}
	if err := h.Store.Hosts.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "host not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// handleHostServiceError translates HostService errors into HTTP
// status codes. ErrInvalidPrivateKey → 400; ErrSecretsUnavailable
// → 503; anything else → 500 with a fixed body string.
//
// 5xx bodies must not echo err.Error() because wrapped messages
// may carry internal paths (e.g. "open /var/run/secrets/...: …") or
// upstream URLs (e.g. "keepsave: https://.../v1/...: 502") that we
// don't want to leak to API clients. The detail is logged server-
// side instead.
func handleHostServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidPrivateKey):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrSecretsUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	default:
		slog.Error("host service error", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
