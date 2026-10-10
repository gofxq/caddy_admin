package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/domain"
	"net/http"
)

func (api *API) portal(c *gin.Context) {
	address := api.application.ClientAddress(c.Request.Context(), c.Request.RemoteAddr, c.Request.Header.Get(domain.ClientAddressHeader))
	services, err := api.application.Portal(c.Request.Context(), address)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, gin.H{"services": services})
}
