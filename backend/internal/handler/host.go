package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cooker-ci/cooker/internal/model"
	"github.com/cooker-ci/cooker/internal/validate"
)

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
	return nil
}

func (h *Handler) ListHosts(c *gin.Context) {
	hosts, err := h.Store.Hosts.List(c.Request.Context())
	if abortStoreErr(c, err, "hosts not found") {
		return
	}
	if hosts == nil {
		hosts = []*model.Host{}
	}
	c.JSON(http.StatusOK, hosts)
}

func (h *Handler) GetHost(c *gin.Context) {
	host, err := h.Store.Hosts.Get(c.Request.Context(), c.Param("id"))
	if abortStoreErr(c, err, "host not found") {
		return
	}
	c.JSON(http.StatusOK, host)
}

func (h *Handler) CreateHost(c *gin.Context) {
	var host model.Host
	if err := c.ShouldBindJSON(&host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
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
	if err := h.Store.Hosts.Create(c.Request.Context(), &host); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, host)
}

func (h *Handler) UpdateHost(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.Store.Hosts.Get(c.Request.Context(), id)
	if abortStoreErr(c, err, "host not found") {
		return
	}
	var host model.Host
	if err := c.ShouldBindJSON(&host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateHostInput(&host); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	host.ID = id
	host.CreatedAt = existing.CreatedAt
	host.UpdatedAt = time.Now()
	if err := h.Store.Hosts.Update(c.Request.Context(), &host); err != nil {
		if abortStoreErr(c, err, "host not found") {
			return
		}
	}
	c.JSON(http.StatusOK, host)
}

func (h *Handler) DeleteHost(c *gin.Context) {
	if err := h.Store.Hosts.Delete(c.Request.Context(), c.Param("id")); err != nil {
		if abortStoreErr(c, err, "host not found") {
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}
