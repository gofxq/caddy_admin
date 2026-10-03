package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func (api *API) publish(c *gin.Context) {
	request, ok := bindJSON[domain.PublishRequest](c)
	if !ok {
		return
	}
	request.ClientAddress = api.application.ClientAddress(c.Request.Context(), c.Request.RemoteAddr, c.GetHeader(domain.ClientAddressHeader))
	deployment, err := api.application.Begin(c.Request.Context(), request, sessionFromContext(c).Username)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusAccepted, deployment)
	slog.Info("deployment_accepted", "request_id", c.Writer.Header().Get("X-Request-ID"), "deployment_id", deployment.ID, "revision", deployment.Revision, "stage", "accepted")
	go api.application.Apply(deployment.ID)
}

func (api *API) deployments(c *gin.Context) {
	offset, limit := page(c)
	items, err := api.application.Deployments(c.Request.Context(), offset, limit)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, gin.H{"items": items, "offset": offset, "limit": limit})
}

func (api *API) deployment(c *gin.Context) {
	deployment, err := api.application.Deployment(c.Request.Context(), c.Param("id"))
	if err != nil {
		abortError(c, err)
		return
	}
	config, redacted, err := configForDisplay(deployment.Config)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, gin.H{"deployment": deployment, "config": config, "config_redacted_fields": redacted})
}

func (api *API) audits(c *gin.Context) {
	offset, limit := page(c)
	items, err := api.application.Audits(c.Request.Context(), offset, limit)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, gin.H{"items": items, "offset": offset, "limit": limit})
}

func (api *API) overview(c *gin.Context) {
	overview, err := api.application.Overview(c.Request.Context())
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, overview)
}
