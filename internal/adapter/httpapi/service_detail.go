package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (api *API) serviceDetail(c *gin.Context) {
	detail, err := api.application.ServiceDetail(c.Request.Context(), c.Param("id"))
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, detail)
}
func (api *API) checkUpstream(c *gin.Context) {
	body, ok := bindJSON[struct {
		ExpectedHash         string `json:"expected_hash"`
		ExpectedDeploymentID string `json:"expected_deployment_id"`
	}](c)
	if !ok {
		return
	}
	result, err := api.application.CheckUpstream(c.Request.Context(), c.Param("id"), body.ExpectedHash, body.ExpectedDeploymentID)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, result)
}
