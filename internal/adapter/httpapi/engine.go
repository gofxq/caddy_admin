package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/domain"
)

const sessionKey = "session"

func newEngine() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false
	engine.Use(func(c *gin.Context) {
		c.Header("X-Request-ID", domain.ID())
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		defer func() {
			if recover() != nil {
				abortError(c, errors.New("internal"))
			}
		}()
		c.Next()
	})
	engine.NoMethod(func(c *gin.Context) {
		abortError(c, &domain.AppError{Status: 405, Code: "method_not_allowed", Message: "请求方法不允许"})
	})
	engine.NoRoute(func(c *gin.Context) {
		abortError(c, &domain.AppError{Status: 404, Code: "not_found", Message: "接口不存在"})
	})
	return engine
}

func respondJSON(c *gin.Context, status int, value any) { c.JSON(status, value) }

func abortError(c *gin.Context, err error) {
	id := c.Writer.Header().Get("X-Request-ID")
	if id == "" {
		id = domain.ID()
		c.Header("X-Request-ID", id)
	}
	slog.Warn("request_failed", "request_id", id, "error_class", domain.ErrorClass(err))
	var applicationError *domain.AppError
	if errors.As(err, &applicationError) {
		c.AbortWithStatusJSON(applicationError.Status, gin.H{"error": gin.H{"code": applicationError.Code, "message": applicationError.Message, "request_id": id}})
		return
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "unavailable", "message": "操作暂不可用，请检查 Caddy 连通性、数据库或部署状态", "request_id": id}})
}

func bindJSON[T any](c *gin.Context) (T, bool) {
	var value T
	if !strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
		abortError(c, domain.Invalid("请求必须为 JSON"))
		return value, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		abortError(c, domain.Invalid("无效请求数据"))
		return value, false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		abortError(c, domain.Invalid("请求包含多余数据"))
		return value, false
	}
	return value, true
}

func sessionFromContext(c *gin.Context) domain.Session { return c.MustGet(sessionKey).(domain.Session) }

func page(c *gin.Context) (int, int) {
	offset, _ := strconv.Atoi(c.Query("offset"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return offset, limit
}

func pathInt64(c *gin.Context, name string) (int64, bool) {
	value, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || value < 0 {
		abortError(c, domain.Invalid("无效草稿版本"))
		return 0, false
	}
	return value, true
}
