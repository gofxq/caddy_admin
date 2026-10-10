package httpapi

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
	"net/http"
	"strconv"
	"time"
)

type observationApplication interface {
	ObservationStatus(context.Context) domain.ObservationStatus
	Traffic(context.Context, domain.TrafficQuery) (domain.Traffic, error)
	AccessLogs(context.Context, domain.LogQuery) ([]domain.AccessEvent, error)
	Alerts(context.Context) ([]domain.Alert, error)
	AcknowledgeAlert(context.Context, string, string) error
	Diagnose(context.Context, string) (domain.Diagnostics, error)
}

func (api *API) observationProvider(c *gin.Context) (observationApplication, bool) {
	p, ok := api.application.(observationApplication)
	if !ok {
		abortError(c, &domain.AppError{Status: 503, Code: "observation_unavailable", Message: "观测暂不可用"})
	}
	return p, ok
}
func (api *API) observationStatus(c *gin.Context) {
	p, ok := api.observationProvider(c)
	if ok {
		respondJSON(c, 200, p.ObservationStatus(c.Request.Context()))
	}
}
func observationRange(c *gin.Context, logs bool) (domain.TrafficQuery, bool) {
	now := time.Now()
	q := domain.TrafficQuery{From: now.Add(-24 * time.Hour), To: now, ServiceID: c.Query("service_id")}
	allowed := map[string]bool{"from": true, "to": true, "service_id": true}
	if logs {
		for _, key := range []string{"method", "status", "path", "ip", "kind", "offset", "limit"} {
			allowed[key] = true
		}
	}
	for k, v := range c.Request.URL.Query() {
		if !allowed[k] || len(v) != 1 {
			abortError(c, domain.Invalid("未知或重复的观测查询参数"))
			return q, false
		}
	}
	for _, p := range []struct {
		name   string
		target *time.Time
	}{{"from", &q.From}, {"to", &q.To}} {
		if value, exists := c.GetQuery(p.name); exists {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 || n > 4102444800 {
				abortError(c, domain.Invalid("时间需使用 Unix 秒"))
				return q, false
			}
			*p.target = time.Unix(n, 0)
		}
	}
	if err := application.ValidateTrafficQuery(q); err != nil {
		abortError(c, err)
		return q, false
	}
	return q, true
}
func (api *API) observationQuerySlot(c *gin.Context) bool {
	select {
	case api.observationSlots <- struct{}{}:
		return true
	default:
		abortError(c, &domain.AppError{Status: 429, Code: "rate_limited", Message: "观测查询繁忙，请稍后重试"})
		return false
	}
}
func (api *API) traffic(c *gin.Context) {
	p, ok := api.observationProvider(c)
	if !ok {
		return
	}
	q, ok := observationRange(c, false)
	if !ok {
		return
	}
	if !api.observationQuerySlot(c) {
		return
	}
	defer func() { <-api.observationSlots }()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	result, err := p.Traffic(ctx, q)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, 200, result)
}
func (api *API) accessLogs(c *gin.Context) {
	p, ok := api.observationProvider(c)
	if !ok {
		return
	}
	r, ok := observationRange(c, true)
	if !ok {
		return
	}
	q := domain.LogQuery{From: r.From, To: r.To, ServiceID: r.ServiceID, Kind: c.DefaultQuery("kind", "access"), Method: c.Query("method"), Path: c.Query("path"), IP: c.Query("ip"), Limit: 50}
	for _, f := range []struct {
		name string
		dst  *int
	}{{"status", &q.Status}, {"limit", &q.Limit}, {"offset", &q.Offset}} {
		if v, exists := c.GetQuery(f.name); exists {
			n, err := strconv.Atoi(v)
			if err != nil {
				abortError(c, domain.Invalid("日志筛选和分页需使用有效数字"))
				return
			}
			*f.dst = n
		}
	}
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 100000 || q.Status != 0 && (q.Status < 100 || q.Status > 599) {
		abortError(c, domain.Invalid("日志分页或状态码无效"))
		return
	}
	if !api.observationQuerySlot(c) {
		return
	}
	defer func() { <-api.observationSlots }()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	items, err := p.AccessLogs(ctx, q)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, 200, gin.H{"items": items, "offset": q.Offset, "limit": q.Limit})
}
func (api *API) alerts(c *gin.Context) {
	p, ok := api.observationProvider(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	items, err := p.Alerts(ctx)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, 200, gin.H{"items": items})
}
func (api *API) acknowledgeAlert(c *gin.Context) {
	if _, ok := bindJSON[struct{}](c); !ok {
		return
	}
	p, ok := api.observationProvider(c)
	if !ok {
		return
	}
	if err := p.AcknowledgeAlert(c.Request.Context(), c.Param("id"), sessionFromContext(c).Username); err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, http.StatusOK, gin.H{"acknowledged": true})
}
func (api *API) diagnose(c *gin.Context) {
	body, ok := bindJSON[struct {
		DomainID string `json:"domain_id"`
	}](c)
	if !ok {
		return
	}
	p, ok := api.observationProvider(c)
	if !ok {
		return
	}
	result, err := p.Diagnose(c.Request.Context(), body.DomainID)
	if err != nil {
		abortError(c, err)
		return
	}
	respondJSON(c, 200, result)
}
