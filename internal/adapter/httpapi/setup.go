package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type SetupApplication interface {
	CheckSetupDNS(context.Context, application.SetupSettings, string) (application.SetupDNSReport, error)
	ConfirmSetupDNS(context.Context, application.SetupSettings, application.SetupCloudflare) (application.SetupDNSPlan, error)
	PreviewSetupDNS(context.Context, string, string, string) (application.SetupDNSPlan, error)
	PreflightSetupImport(context.Context, application.SetupSettings, []application.PortableService) application.SetupPreflight
	SetupStatus(context.Context) (application.SetupStatus, error)
	PreflightSetup(context.Context, application.SetupSettings) application.SetupPreflight
	CompleteSetup(context.Context, application.SetupRequest) (string, error)
}

type SetupOptions struct{}

type setupAPI struct {
	application SetupApplication
	done        func()
	mu          sync.Mutex
	attempts    map[string][]time.Time
}

func NewSetup(application SetupApplication, _ SetupOptions, done func()) http.Handler {
	api := &setupAPI{application: application, done: done, attempts: map[string][]time.Time{}}
	router := newEngine()
	router.Use(func(c *gin.Context) {
		if !api.allow(c.Request) {
			abortError(c, &domain.AppError{Status: 429, Code: "rate_limited", Message: "请求过多，请稍后重试"})
			return
		}
		c.Next()
	})
	router.GET("/api/v1/setup/status", api.status)
	router.POST("/api/v1/setup/preflight", api.preflight)
	router.POST("/api/v1/setup/complete", api.complete)
	router.POST("/api/v1/setup/dns/preview", api.dnsPreview)
	router.POST("/api/v1/setup/dns/confirm", api.dnsConfirm)
	router.POST("/api/v1/setup/dns/check", api.dnsCheck)
	return router
}

func (api *setupAPI) allow(request *http.Request) bool {
	host, _, _ := net.SplitHostPort(request.RemoteAddr)
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	api.mu.Lock()
	defer api.mu.Unlock()
	old := api.attempts[host]
	kept := old[:0]
	for _, attempt := range old {
		if attempt.After(cutoff) {
			kept = append(kept, attempt)
		}
	}
	if len(kept) >= 20 {
		api.attempts[host] = kept
		return false
	}
	api.attempts[host] = append(kept, now)
	return true
}

func (api *setupAPI) status(c *gin.Context) {
	status, err := api.application.SetupStatus(c.Request.Context())
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, status)
}

func (api *setupAPI) preflight(c *gin.Context) {
	if !setupOriginMatches(c.GetHeader("Origin"), c.Request.Host) {
		abortError(c, &domain.AppError{Status: 403, Code: "origin", Message: "请求来源不匹配"})
		return
	}
	request, ok := bindJSON[struct {
		Settings       application.SetupSettings     `json:"settings"`
		ImportSettings *domain.ManagedSettings       `json:"import_settings,omitempty"`
		Services       []application.PortableService `json:"services,omitempty"`
	}](c)
	if !ok {
		return
	}
	request.Settings.ImportSettings = request.ImportSettings
	if len(request.Services) > 0 {
		respondJSON(c, http.StatusOK, api.application.PreflightSetupImport(c.Request.Context(), request.Settings, request.Services))
		return
	}
	respondJSON(c, http.StatusOK, api.application.PreflightSetup(c.Request.Context(), request.Settings))
}

func (api *setupAPI) complete(c *gin.Context) {
	if !setupOriginMatches(c.GetHeader("Origin"), c.Request.Host) {
		abortError(c, &domain.AppError{Status: 403, Code: "origin", Message: "请求来源不匹配"})
		return
	}
	request, ok := bindJSON[application.SetupRequest](c)
	if !ok {
		return
	}
	origin, err := api.application.CompleteSetup(c.Request.Context(), request)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusAccepted, gin.H{"status": "restarting", "admin_origin": origin})
	if api.done != nil {
		go api.done()
	}
}

func setupOriginMatches(actual, requestHost string) bool {
	parsed, err := url.Parse(actual)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.EqualFold(parsed.Host, requestHost) {
		return false
	}
	return true
}

func (api *setupAPI) dnsPreview(c *gin.Context) {
	if !setupOriginMatches(c.GetHeader("Origin"), c.Request.Host) {
		abortError(c, &domain.AppError{Status: 403, Code: "origin", Message: "请求来源不匹配"})
		return
	}
	request, ok := bindJSON[struct {
		Token              string `json:"token"`
		Domain             string `json:"domain"`
		UseConfiguredToken bool   `json:"use_configured_token,omitempty"`
		SetupID            string `json:"setup_id,omitempty"`
		Address            string `json:"address"`
	}](c)
	if !ok {
		return
	}
	token := request.Token
	if resolver, ok := api.application.(interface {
		ResolveSetupToken(string, bool, string) (string, error)
	}); ok {
		var err error
		token, err = resolver.ResolveSetupToken(request.Token, request.UseConfiguredToken, request.SetupID)
		if err != nil {
			abortError(c, err)
			return
		}
	} else if request.UseConfiguredToken {
		provider, ok := api.application.(interface{ ConfiguredSetupToken(string) (string, error) })
		if !ok {
			abortError(c, domain.Invalid("预配置凭据不可用"))
			return
		}
		var err error
		token, err = provider.ConfiguredSetupToken(request.SetupID)
		if err != nil {
			abortError(c, err)
			return
		}
	}
	plan, err := api.application.PreviewSetupDNS(c.Request.Context(), token, request.Domain, request.Address)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, plan)
}

func (api *setupAPI) dnsConfirm(c *gin.Context) {
	if !setupOriginMatches(c.GetHeader("Origin"), c.Request.Host) {
		abortError(c, &domain.AppError{Status: 403, Code: "origin", Message: "请求来源不匹配"})
		return
	}
	request, ok := bindJSON[struct {
		Settings           application.SetupSettings   `json:"settings"`
		Cloudflare         application.SetupCloudflare `json:"cloudflare"`
		ImportSettings     *domain.ManagedSettings     `json:"import_settings,omitempty"`
		UseConfiguredToken bool                        `json:"use_configured_token,omitempty"`
		SetupID            string                      `json:"setup_id,omitempty"`
	}](c)
	if !ok {
		return
	}
	request.Settings.ImportSettings = request.ImportSettings
	if request.UseConfiguredToken {
		request.Cloudflare.UseConfiguredToken, request.Cloudflare.SetupID = true, request.SetupID
	}
	plan, err := api.application.ConfirmSetupDNS(c.Request.Context(), request.Settings, request.Cloudflare)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, plan)
}

func (api *setupAPI) dnsCheck(c *gin.Context) {
	if !setupOriginMatches(c.GetHeader("Origin"), c.Request.Host) {
		abortError(c, &domain.AppError{Status: 403, Code: "origin", Message: "请求来源不匹配"})
		return
	}
	request, ok := bindJSON[struct {
		Settings application.SetupSettings `json:"settings"`
		Address  string                    `json:"address"`
	}](c)
	if !ok {
		return
	}
	report, err := api.application.CheckSetupDNS(c.Request.Context(), request.Settings, request.Address)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, report)
}
