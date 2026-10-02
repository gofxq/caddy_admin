package httpapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type Application interface {
	ExportConfiguration(context.Context) (application.Configuration, error)
	PreviewConfiguration(context.Context, application.Configuration) (application.ConfigurationPreview, error)
	ImportConfiguration(context.Context, application.Configuration, int64, bool, string) (domain.Draft, error)
	LoginAllowed(context.Context, string) bool
	Login(context.Context, string, string, string) (domain.Session, error)
	Session(context.Context, string) (domain.Session, error)
	Logout(context.Context, string) error
	ChangePassword(context.Context, string, string) error
	ClientAddress(context.Context, string, string) string
	Draft(context.Context) (domain.Draft, error)
	Published(context.Context) ([]domain.Service, error)
	SaveService(context.Context, int64, domain.Service, bool, string) (domain.Draft, error)
	Preview(context.Context, string) (domain.Preview, error)
	Validate(context.Context, int64, string, string) (domain.Preview, error)
	DraftRevision(context.Context, int64) (domain.Draft, error)
	Begin(context.Context, domain.PublishRequest, string) (domain.Deployment, error)
	Apply(string)
	Deployments(context.Context, int, int) ([]domain.Deployment, error)
	Deployment(context.Context, string) (domain.Deployment, error)
	Audits(context.Context, int, int) ([]domain.AuditEvent, error)
	Overview(context.Context) (domain.Overview, error)
	BuildInfo(context.Context) (string, bool)
	CertificateState(context.Context) (domain.CertificateStatus, error)
	TokenConfigured() bool
	ActivateCloudflare(context.Context, string, string) (domain.CertificateStatus, error)
	Certificates(context.Context) []domain.Certificate
}

type Options struct {
	Origin         string
	ManagerVersion string
	RuntimeConfig  any
	ExternalCaddy  bool
}

type API struct {
	application  Application
	options      Options
	loginMu      sync.Mutex
	loginClients map[string]bool
	authSlots    chan struct{}
	certMu       sync.Mutex
	certificates []domain.Certificate
	certTime     time.Time
	restart      func()
	rescueEntry  atomic.Bool
}

func New(application Application, options Options) *API {
	return &API{application: application, options: options, authSlots: make(chan struct{}, 2), loginClients: map[string]bool{}}
}

func (api *API) Handler() http.Handler {
	router := newEngine()
	router.POST("/api/v1/auth/login", api.login)
	authenticated := router.Group("/api/v1", api.authenticate())
	authenticated.GET("/auth/session", func(c *gin.Context) { respondJSON(c, http.StatusOK, sessionFromContext(c)) })
	authenticated.POST("/auth/logout", api.logout)
	authenticated.POST("/auth/password", api.password)
	management := authenticated.Group("", api.requireChangedPassword())
	management.GET("/overview", api.overview)
	management.GET("/services", api.services)
	management.POST("/services", api.saveService)
	management.PUT("/services/:id", api.saveService)
	management.DELETE("/services/:id", api.saveService)
	management.GET("/draft/preview", api.preview)
	management.GET("/draft/revisions/:revision", api.draftRevision)
	management.POST("/draft/validate", api.validate)
	management.POST("/deployments", api.publish)
	management.GET("/deployments", api.deployments)
	management.GET("/deployments/:id", api.deployment)
	management.GET("/audit", api.audits)
	management.GET("/configuration/export", api.exportConfiguration)
	management.POST("/configuration/preview", api.previewConfiguration)
	management.POST("/configuration/import", api.importConfiguration)
	management.GET("/settings", api.settings)
	management.POST("/settings/cloudflare", api.activateCloudflare)
	management.GET("/certificates", api.certificateList)
	return router
}

func (api *API) SetRestart(callback func())         { api.restart = callback }
func (api *API) SetRescueEntryEnabled(enabled bool) { api.rescueEntry.Store(enabled) }
func (api *API) originAllowed(request *http.Request) bool {
	origin := strings.TrimRight(request.Header.Get("Origin"), "/")
	return origin == strings.TrimRight(api.options.Origin, "/") || (api.rescueEntry.Load() && setupOriginMatches(origin, request.Host))
}

func (api *API) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Request.Cookie("__Host-session")
		if err != nil {
			abortError(c, &domain.AppError{Status: 401, Code: "unauthorized", Message: "请先登录"})
			return
		}
		session, err := api.application.Session(c.Request.Context(), cookie.Value)
		if err != nil {
			abortError(c, err)
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			if !api.originAllowed(c.Request) || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
				abortError(c, &domain.AppError{Status: 403, Code: "csrf", Message: "请求来源或 CSRF 校验失败"})
				return
			}
		}
		c.Set(sessionKey, session)
		c.Next()
	}
}

func (api *API) requireChangedPassword() gin.HandlerFunc {
	return func(c *gin.Context) {
		if sessionFromContext(c).MustChange {
			abortError(c, &domain.AppError{Status: 403, Code: "password_change_required", Message: "请先修改初始密码"})
			return
		}
		c.Next()
	}
}
