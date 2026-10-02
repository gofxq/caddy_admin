package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func (api *API) login(c *gin.Context) {
	if !api.originAllowed(c.Request) {
		abortError(c, &domain.AppError{Status: 403, Code: "origin", Message: "请求来源不匹配"})
		return
	}
	clientAddress := api.application.ClientAddress(c.Request.Context(), c.Request.RemoteAddr, c.GetHeader(domain.ClientAddressHeader))
	api.loginMu.Lock()
	busy := api.loginClients[clientAddress]
	if !busy {
		api.loginClients[clientAddress] = true
	}
	api.loginMu.Unlock()
	if busy {
		_ = api.application.LoginAllowed(c.Request.Context(), clientAddress)
		abortError(c, &domain.AppError{Status: 429, Code: "rate_limited", Message: "该客户端已有登录请求，请稍后重试"})
		return
	}
	defer func() {
		api.loginMu.Lock()
		delete(api.loginClients, clientAddress)
		api.loginMu.Unlock()
	}()
	if !api.application.LoginAllowed(c.Request.Context(), clientAddress) {
		abortError(c, &domain.AppError{Status: 429, Code: "rate_limited", Message: "尝试过多，请 5 分钟后再试"})
		return
	}
	select {
	case api.authSlots <- struct{}{}:
		defer func() { <-api.authSlots }()
	default:
		abortError(c, &domain.AppError{Status: 429, Code: "rate_limited", Message: "请稍后重试"})
		return
	}
	body, ok := bindJSON[struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}](c)
	if !ok {
		return
	}
	session, err := api.application.Login(c.Request.Context(), body.Username, body.Password, clientAddress)
	if err != nil {
		abortError(c, err)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "__Host-session", Value: session.Token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(domain.SessionLifetime / time.Second)})
	respondJSON(c, http.StatusOK, session)
}

func (api *API) logout(c *gin.Context) {
	session := sessionFromContext(c)
	if err := api.application.Logout(c.Request.Context(), session.Token); err != nil {
		abortError(c, err)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "__Host-session", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	respondJSON(c, http.StatusOK, gin.H{"ok": true})
}

func (api *API) password(c *gin.Context) {
	body, ok := bindJSON[struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}](c)
	if !ok {
		return
	}
	select {
	case api.authSlots <- struct{}{}:
		defer func() { <-api.authSlots }()
	default:
		abortError(c, &domain.AppError{Status: 429, Code: "rate_limited", Message: "请稍后重试"})
		return
	}
	if err := api.application.ChangePassword(c.Request.Context(), body.Current, body.Password); err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, gin.H{"ok": true})
}
