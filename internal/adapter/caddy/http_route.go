package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func (client *Client) AdaptHTTPRoute(ctx context.Context, source string) (json.RawMessage, error) {
	if len(source) > 16<<10 {
		return nil, domain.Invalid("服务 Caddyfile 超过 16 KiB")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, client.options.CaddyBinary, "manager-adapt-http-route")
	cmd.Env = []string{"LANG=C"}
	cmd.Stdin = strings.NewReader(source)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.New("无法启动 Caddy 服务片段适配器")
	}
	const limit = 1 << 20
	raw, readErr := io.ReadAll(io.LimitReader(pipe, limit+1))
	pipe.Close()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if readErr != nil || len(raw) > limit {
		return nil, domain.Invalid("服务路由输出超过限制或无法读取")
	}
	if err != nil {
		var exit *exec.ExitError
		// Only our private command returns these bounded, sanitized diagnostics on stdout.
		if errors.As(err, &exit) && exit.ExitCode() == 2 && len(raw) > 0 && len(raw) <= 512 {
			return nil, domain.Invalid(strings.TrimSpace(string(raw)))
		}
		return nil, errors.New("无法使用 Caddy 服务片段适配器，请检查 Caddy 二进制版本")
	}
	var routes []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &routes); err != nil || len(routes) == 0 {
		return nil, domain.Invalid("Caddy 未返回有效的服务路由")
	}
	for _, route := range routes {
		if len(route) == 0 {
			return nil, domain.Invalid("Caddy 返回空服务路由")
		}
	}
	return bytes.TrimSpace(raw), nil
}
