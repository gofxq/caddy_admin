package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/application"
)

type HandoffApplication interface {
	SetupHandoff(context.Context) application.SetupHandoff
}

type handoffAPI struct {
	application HandoffApplication
	ready       func()
	mu          sync.Mutex
	attempts    map[string][]time.Time
	lan         []netip.Prefix
}

type HandoffOptions struct{ LAN []string }

func NewHandoff(application HandoffApplication, options HandoffOptions, ready func()) http.Handler {
	api := &handoffAPI{application: application, ready: ready, attempts: map[string][]time.Time{}}
	for _, value := range options.LAN {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			api.lan = append(api.lan, prefix)
		}
	}
	router := newEngine()
	router.GET("/api/v1/setup/handoff", api.handoff)
	return router
}

func remoteAddress(remote string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return netip.Addr{}, false
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	address = address.Unmap()
	return address, true
}

func PrivateSetupHost(hostport string) bool {
	host := hostport
	if parsed, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	address = address.Unmap()
	return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast()
}

func (api *handoffAPI) sourcePermitted(address netip.Addr) bool {
	if address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() {
		return true
	}
	for _, prefix := range api.lan {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (api *handoffAPI) allowed(remote string) (bool, bool) {
	address, ok := remoteAddress(remote)
	if !ok || !api.sourcePermitted(address) {
		return false, false
	}
	key := address.String()
	now, cutoff := time.Now(), time.Now().Add(-time.Minute)
	api.mu.Lock()
	defer api.mu.Unlock()
	kept := api.attempts[key][:0]
	for _, attempt := range api.attempts[key] {
		if attempt.After(cutoff) {
			kept = append(kept, attempt)
		}
	}
	if len(kept) >= 60 {
		api.attempts[key] = kept
		return false, true
	}
	api.attempts[key] = append(kept, now)
	return true, true
}

func (api *handoffAPI) handoff(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !PrivateSetupHost(c.Request.Host) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "forbidden", "message": "临时交接入口仅接受私有地址 Host"}})
		return
	}
	allowed, permitted := api.allowed(c.Request.RemoteAddr)
	if !allowed {
		status := http.StatusForbidden
		if permitted {
			status = http.StatusTooManyRequests
		}
		c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": "forbidden", "message": "临时交接入口仅供可信网络访问"}})
		return
	}
	status := api.application.SetupHandoff(c.Request.Context())
	respondJSON(c, http.StatusOK, status)
	if status.ConsoleStatus == "ready" && api.ready != nil {
		api.ready()
	}
}
