package httpapi

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
	"net/http"
)

type domainDNSApplication interface {
	PreviewDomainDNS(context.Context, string, string) (application.SetupDNSPlan, error)
	ConfirmDomainDNS(context.Context, string, string, string, bool, string) (application.SetupDNSPlan, error)
}
type domainDNSRequest struct {
	DomainID    string `json:"domain_id"`
	Address     string `json:"address"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Confirm     bool   `json:"confirm,omitempty"`
}

func (api *API) previewDomainDNS(c *gin.Context) {
	body, ok := bindJSON[domainDNSRequest](c)
	if !ok {
		return
	}
	provider, ok := api.application.(domainDNSApplication)
	if !ok {
		abortError(c, domain.Invalid("DNS 管理不可用"))
		return
	}
	plan, err := provider.PreviewDomainDNS(c.Request.Context(), body.DomainID, body.Address)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, plan)
}
func (api *API) confirmDomainDNS(c *gin.Context) {
	body, ok := bindJSON[domainDNSRequest](c)
	if !ok {
		return
	}
	provider, ok := api.application.(domainDNSApplication)
	if !ok {
		abortError(c, domain.Invalid("DNS 管理不可用"))
		return
	}
	plan, err := provider.ConfirmDomainDNS(c.Request.Context(), body.DomainID, body.Address, body.Fingerprint, body.Confirm, sessionFromContext(c).Username)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, plan)
}
