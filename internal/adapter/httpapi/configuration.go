package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/application"
	"net/http"
)

func (api *API) exportConfiguration(c *gin.Context) {
	value, err := api.application.ExportConfiguration(c.Request.Context())
	if err != nil {
		abortError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respondJSON(c, http.StatusOK, value)
}
func (api *API) previewConfiguration(c *gin.Context) {
	body, ok := bindJSON[struct {
		Configuration application.Configuration `json:"configuration"`
	}](c)
	if !ok {
		return
	}
	value, err := api.application.PreviewConfiguration(c.Request.Context(), body.Configuration)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, value)
}
func (api *API) importConfiguration(c *gin.Context) {
	body, ok := bindJSON[struct {
		Configuration application.Configuration `json:"configuration"`
		Revision      int64                     `json:"revision"`
		Confirm       bool                      `json:"confirm"`
	}](c)
	if !ok {
		return
	}
	value, err := api.application.ImportConfiguration(c.Request.Context(), body.Configuration, body.Revision, body.Confirm, sessionFromContext(c).Username)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, value)
}
