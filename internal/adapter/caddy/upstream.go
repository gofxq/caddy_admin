package caddy

import (
	"context"
	"net"
	"time"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

// TCPUpstreamProbe connects only to the address already checked by the use
// case. A TCP connection does not validate HTTP behavior or HTTPS certificates.
type TCPUpstreamProbe struct{}

var _ application.UpstreamProbe = (*TCPUpstreamProbe)(nil)

func (*TCPUpstreamProbe) Check(ctx context.Context, target string) domain.UpstreamCheck {
	started := time.Now()
	result := domain.UpstreamCheck{Status: "unknown", Target: target, Vantage: "manager"}
	connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", target)
	result.DurationMS = time.Since(started).Milliseconds()
	if err == nil {
		_ = connection.Close()
		result.Status = "reachable"
		result.Message = "Manager 可建立 TCP 连接；不代表 HTTP 应用正常或 HTTPS 证书有效"
	} else if ctx.Err() != nil {
		result.Message = "检查超时或已取消，结果未知，可稍后重试"
	} else {
		result.Status = "unreachable"
		result.Message = "Manager 无法建立 TCP 连接，请检查上游监听、网络和防火墙后重试"
	}
	return result
}
