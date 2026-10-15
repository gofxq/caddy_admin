package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func (api *API) checkService(c *gin.Context) {
	body, ok := bindJSON[struct {
		Revision int64          `json:"revision"`
		Service  domain.Service `json:"service"`
	}](c)
	if !ok {
		return
	}
	checker, ok := api.application.(interface {
		CheckService(context.Context, int64, domain.Service) (application.ServiceConfigurationCheck, error)
	})
	if !ok {
		abortError(c, &domain.AppError{Status: 503, Code: "unavailable", Message: "服务配置检查暂不可用，请稍后重试"})
		return
	}
	result, err := checker.CheckService(c.Request.Context(), body.Revision, body.Service)
	if err != nil {
		abortError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respondJSON(c, http.StatusOK, result)
}
