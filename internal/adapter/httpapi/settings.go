package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type managedSettingsApplication interface {
	RuntimeSettings(context.Context) (domain.ManagedSettings, error)
	SaveSettings(context.Context, int64, domain.ManagedSettings, bool, string, string) (domain.Draft, error)
	CompleteConsoleTransition(context.Context, int64, bool, string, string) (domain.Draft, error)
}

func (api *API) settings(c *gin.Context) {
	version, module := api.application.BuildInfo(c.Request.Context())
	status, err := api.application.CertificateState(c.Request.Context())
	if err != nil {
		abortError(c, err)
		return
	}
	config, activeConfig := api.options.RuntimeConfig, api.options.RuntimeConfig
	revision := int64(0)
	if provider, ok := api.application.(managedSettingsApplication); ok {
		draft, e := api.application.Draft(c.Request.Context())
		if e != nil {
			abortError(c, e)
			return
		}
		active, e := provider.RuntimeSettings(c.Request.Context())
		if e != nil {
			abortError(c, e)
			return
		}
		config, activeConfig, revision = draft.Settings, active, draft.Revision
	}
	withTLS := func(value any) any {
		raw, _ := json.Marshal(value)
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		if tls, ok := api.options.RuntimeConfig.(interface{ IsTestTLS() bool }); ok {
			out["test_tls"] = tls.IsTestTLS()
		} else {
			r, _ := json.Marshal(api.options.RuntimeConfig)
			var deployment map[string]any
			_ = json.Unmarshal(r, &deployment)
			out["test_tls"] = deployment["test_tls"] == true
		}
		return out
	}
	respondJSON(c, http.StatusOK, gin.H{"config": withTLS(config), "active_config": withTLS(activeConfig), "revision": revision, "manager_version": api.options.ManagerVersion, "caddy_version": version, "cloudflare_module": module, "token_configured": api.application.TokenConfigured(), "certificate_status": status, "external_caddy": api.options.ExternalCaddy})
}

func (api *API) saveSettings(c *gin.Context) {
	body, ok := bindJSON[struct {
		Revision        int64                  `json:"revision"`
		Settings        domain.ManagedSettings `json:"settings"`
		ConfirmExposure bool                   `json:"confirm_exposure"`
	}](c)
	if !ok {
		return
	}
	provider, ok := api.application.(managedSettingsApplication)
	if !ok {
		abortError(c, domain.Invalid("设置管理不可用"))
		return
	}
	client := api.application.ClientAddress(c.Request.Context(), c.Request.RemoteAddr, c.GetHeader(domain.ClientAddressHeader))
	draft, err := provider.SaveSettings(c.Request.Context(), body.Revision, body.Settings, body.ConfirmExposure, sessionFromContext(c).Username, client)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, draft)
}

func (api *API) completeConsoleTransition(c *gin.Context) {
	body, ok := bindJSON[struct {
		Revision int64 `json:"revision"`
		Confirm  bool  `json:"confirm"`
	}](c)
	if !ok {
		return
	}
	provider, ok := api.application.(managedSettingsApplication)
	if !ok {
		abortError(c, domain.Invalid("设置管理不可用"))
		return
	}
	draft, err := provider.CompleteConsoleTransition(c.Request.Context(), body.Revision, body.Confirm, c.GetHeader("Origin"), sessionFromContext(c).Username)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, draft)
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
