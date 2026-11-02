package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/catalog"
)

func (h *Handler) catalogService() *catalog.Service {
	s := &catalog.Service{Store: h.Store}
	if h.Vercel != nil {
		s.Vercel = h.Vercel
	}
	if h.VercelPublisher != nil {
		s.Publications = h.VercelPublisher
	}
	return s
}
func catalogWrite(c *gin.Context) bool {
	u := auth.GetUser(c)
	if u == nil {
		return false
	}
	for _, r := range u.Roles {
		if r == string(auth.RoleAdmin) || r == string(auth.RoleOperator) {
			return true
		}
	}
	return false
}
func catalogQuery(c *gin.Context) catalog.Query {
	return catalog.Query{Search: c.Query("q"), Provider: c.Query("provider"), Management: c.Query("management"), Environment: c.Query("environment"), Status: c.Query("status"), AppID: c.Query("appId"), Limit: intQuery(c, "limit", listDefaultLimit, 1, listMaxLimit), Offset: intQuery(c, "offset", 0, 0, 1<<30)}
}
func catalogError(c *gin.Context, err error) {
	if errors.Is(err, catalog.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Catalog resource not found"})
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Catalog source is temporarily unavailable; retry after synchronization"})
}
func (h *Handler) ListCatalogApps(c *gin.Context) {
	c.JSON(http.StatusOK, h.catalogService().Apps(c.Request.Context(), catalogQuery(c), catalogWrite(c)))
}
func (h *Handler) GetCatalogApp(c *gin.Context) {
	v, err := h.catalogService().App(c.Request.Context(), c.Param("id"), catalogWrite(c))
	if err != nil {
		catalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}
func (h *Handler) ListCatalogServices(c *gin.Context) {
	c.JSON(http.StatusOK, h.catalogService().Services(c.Request.Context(), catalogQuery(c), catalogWrite(c)))
}
func (h *Handler) ListCatalogPipelines(c *gin.Context) {
	c.JSON(http.StatusOK, h.catalogService().Pipelines(c.Request.Context(), catalogQuery(c), catalogWrite(c)))
}
func (h *Handler) GetCatalogPipeline(c *gin.Context) {
	v, err := h.catalogService().Pipeline(c.Request.Context(), c.Param("id"), catalogWrite(c))
	if err != nil {
		catalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}
