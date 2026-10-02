package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func (api *API) services(c *gin.Context) {
	draft, err := api.application.Draft(c.Request.Context())
	if err != nil {
		abortError(c, err)
		return
	}
	published, err := api.application.Published(c.Request.Context())
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, gin.H{"revision": draft.Revision, "services": draft.Services, "published": published})
}

func (api *API) saveService(c *gin.Context) {
	body, ok := bindJSON[struct {
		Revision int64          `json:"revision"`
		Service  domain.Service `json:"service"`
	}](c)
	if !ok {
		return
	}
	if id := c.Param("id"); id != "" {
		body.Service.ID = id
	} else {
		body.Service.ID = ""
	}
	draft, err := api.application.SaveService(c.Request.Context(), body.Revision, body.Service, c.Request.Method == http.MethodDelete, sessionFromContext(c).Username)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, draft)
}

func (api *API) preview(c *gin.Context) {
	preview, err := api.application.Preview(c.Request.Context(), c.Query("rollback"))
	if err != nil {
		abortError(c, err)
		return
	}
	display, err := previewForDisplay(preview)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, display)
}

func (api *API) validate(c *gin.Context) {
	body, ok := bindJSON[struct {
		Revision int64  `json:"revision"`
		Rollback string `json:"rollback_id"`
	}](c)
	if !ok {
		return
	}
	preview, err := api.application.Validate(c.Request.Context(), body.Revision, body.Rollback, sessionFromContext(c).Username)
	if err != nil {
		slog.Warn("validation_failed", "request_id", c.Writer.Header().Get("X-Request-ID"), "revision", preview.Revision, "stage", "validation", "error_class", domain.ErrorClass(err))
		abortError(c, err)
		return
	}
	display, err := previewForDisplay(preview)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, display)
}

func (api *API) draftRevision(c *gin.Context) {
	revision, ok := pathInt64(c, "revision")
	if !ok {
		return
	}
	draft, err := api.application.DraftRevision(c.Request.Context(), revision)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, draft)
}
