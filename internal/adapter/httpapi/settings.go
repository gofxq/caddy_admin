package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func (api *API) settings(c *gin.Context) {
	version, module := api.application.BuildInfo(c.Request.Context())
	status, err := api.application.CertificateState(c.Request.Context())
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, gin.H{"config": api.options.RuntimeConfig, "manager_version": api.options.ManagerVersion, "caddy_version": version, "cloudflare_module": module, "token_configured": api.application.TokenConfigured(), "certificate_status": status, "external_caddy": api.options.ExternalCaddy})
}

func (api *API) activateCloudflare(c *gin.Context) {
	body, ok := bindJSON[struct {
		Token  string `json:"token"`
		Enable bool   `json:"enable"`
	}](c)
	if !ok {
		return
	}
	if !body.Enable {
		abortError(c, domain.Invalid("证书切换暂不支持关闭；请使用运维恢复流程"))
		return
	}
	status, err := api.application.ActivateCloudflare(c.Request.Context(), body.Token, sessionFromContext(c).Username)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusAccepted, gin.H{"certificate_status": status, "restart_required": api.restart != nil})
	if api.restart != nil {
		go api.restart()
	}
}

func (api *API) certificateList(c *gin.Context) {
	api.certMu.Lock()
	defer api.certMu.Unlock()
	if time.Since(api.certTime) > time.Minute {
		api.certificates = api.application.Certificates(c.Request.Context())
		api.certTime = time.Now()
	}
	respondJSON(c, http.StatusOK, gin.H{"items": api.certificates})
}
