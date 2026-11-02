package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/provider/neon"
)

func (h *Handler) neonAccess(c *gin.Context, admin bool) bool {
	u := auth.GetUser(c)
	if u == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return false
	}
	if admin {
		allowed := false
		for _, role := range u.Roles {
			if role == string(auth.RoleAdmin) {
				allowed = true
			}
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "administrator access required"})
			return false
		}
	}
	if h.Neon == nil {
		neonError(c, &neon.RequestError{Code: "unavailable"})
		return false
	}
	return true
}

func neonError(c *gin.Context, err error) {
	code, status := "unavailable", http.StatusServiceUnavailable
	var e *neon.RequestError
	if errors.As(err, &e) {
		switch e.Code {
		case "invalid_input", "connection_limit":
			code, status = e.Code, http.StatusUnprocessableEntity
		case "not_found":
			code, status = e.Code, http.StatusNotFound
		case "conflict", "busy":
			code, status = e.Code, http.StatusConflict
		case "paused", "scope_mismatch":
			code, status = e.Code, http.StatusUnprocessableEntity
		case "canceled":
			code, status = e.Code, http.StatusRequestTimeout
		case "provider_unauthorized", "provider_denied", "invalid_response", "provider_unavailable":
			code, status = e.Code, http.StatusBadGateway
		case "rate_limited":
			code, status = e.Code, http.StatusTooManyRequests
		}
	}
	c.JSON(status, gin.H{"code": code, "error": (&neon.RequestError{Code: code}).Error()})
}

// No request body or provider diagnostic is logged. Reject duplicate/null and
// unknown fields before accepting a credential/configuration generation.
func decodeNeonRequest(c *gin.Context, out any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	body, err := io.ReadAll(c.Request.Body)
	defer clear(body)
	reject := func() bool {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid Neon connection request"})
		return false
	}
	if err != nil {
		return reject()
	}
	allowed := map[string]bool{"name": true, "accountMode": true, "apiKey": true, "projectIds": true, "acknowledgeKeyPermissions": true}
	switch out.(type) {
	case *neon.CreateConnectionRequest:
		allowed["organizationId"] = true
	case *neon.UpdateConnectionRequest:
		allowed["version"] = true
		allowed["enabled"] = true
	default:
		return reject()
	}
	d := json.NewDecoder(bytes.NewReader(body))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return reject()
	}
	seen := map[string]bool{}
	for d.More() {
		t, err = d.Token()
		if err != nil {
			return reject()
		}
		name, ok := t.(string)
		if !ok || !allowed[name] {
			return reject()
		}
		if seen[name] {
			return reject()
		}
		seen[name] = true
		var raw json.RawMessage
		parseErr := d.Decode(&raw)
		isNull := bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
		clear(raw)
		if parseErr != nil || isNull {
			return reject()
		}
	}
	if _, err = d.Token(); err != nil {
		return reject()
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return reject()
	}
	d = json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return reject()
	}
	return true
}

func neonVersion(c *gin.Context) (int, bool) {
	values := c.Request.URL.Query()["version"]
	if len(values) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "one connection version is required"})
		return 0, false
	}
	v, err := strconv.Atoi(values[0])
	if err != nil || v < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid connection version is required"})
		return 0, false
	}
	return v, true
}

func (h *Handler) ListNeonConnections(c *gin.Context) {
	if !h.neonAccess(c, false) {
		return
	}
	rows, err := h.Neon.List(c.Request.Context())
	if err != nil {
		neonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"connections": rows, "providerOperations": "metadata_reads_only", "databaseHealth": "not_checked", "managementModeEvidence": "operator_declared"})
}
func (h *Handler) GetNeonConnection(c *gin.Context) {
	if !h.neonAccess(c, false) {
		return
	}
	row, err := h.Neon.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		neonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"connection": row})
}
func (h *Handler) CreateNeonConnection(c *gin.Context) {
	if !h.neonAccess(c, true) {
		return
	}
	var req neon.CreateConnectionRequest
	if !decodeNeonRequest(c, &req) {
		return
	}
	row, err := h.Neon.Create(c.Request.Context(), req)
	if err != nil {
		neonError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"connection": row})
}
func (h *Handler) UpdateNeonConnection(c *gin.Context) {
	if !h.neonAccess(c, true) {
		return
	}
	var req neon.UpdateConnectionRequest
	if !decodeNeonRequest(c, &req) {
		return
	}
	row, err := h.Neon.Update(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		neonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"connection": row})
}
func (h *Handler) DeleteNeonConnection(c *gin.Context) {
	if !h.neonAccess(c, true) {
		return
	}
	v, ok := neonVersion(c)
	if !ok {
		return
	}
	if err := h.Neon.Delete(c.Request.Context(), c.Param("id"), v); err != nil {
		neonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"disconnected": true, "providerResourcesChanged": false})
}
func (h *Handler) SyncNeonConnection(c *gin.Context) {
	if !h.neonAccess(c, true) {
		return
	}
	row, err := h.Neon.Sync(c.Request.Context(), c.Param("id"))
	if err != nil {
		neonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"connection": row})
}

func (h *Handler) DiscoverNeonProjects(c *gin.Context) {
	if !h.neonAccess(c, true) {
		return
	}
	v, ok := neonVersion(c)
	if !ok {
		return
	}
	data, err := h.Neon.DiscoverProjects(c.Request.Context(), c.Param("id"), v)
	if err != nil {
		neonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"inventory": data, "selectionChanged": false, "databaseHealth": "not_checked"})
}
func (h *Handler) GetNeonDatabases(c *gin.Context) {
	if !h.neonAccess(c, true) {
		return
	}
	v, ok := neonVersion(c)
	if !ok {
		return
	}
	data, err := h.Neon.ConnectionDatabases(c.Request.Context(), c.Param("id"), v, c.Param("projectId"), c.Param("branchId"))
	if err != nil {
		neonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"inventory": data, "databaseHealth": "not_checked"})
}
func (h *Handler) GetNeonEndpoints(c *gin.Context) {
	if !h.neonAccess(c, true) {
		return
	}
	v, ok := neonVersion(c)
	if !ok {
		return
	}
	data, err := h.Neon.ConnectionEndpoints(c.Request.Context(), c.Param("id"), v, c.Param("projectId"))
	if err != nil {
		neonError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"inventory": data, "databaseHealth": "not_checked"})
}
