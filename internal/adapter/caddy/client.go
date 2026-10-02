package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/config"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type Options struct {
	AdminURL    string
	Socket      string
	CaddyBinary string
	DataDir     string
}

type Client struct {
	options Options
	http    *http.Client
}

type LoadRejected struct{}

func (*LoadRejected) Error() string    { return "Caddy 拒绝配置；旧配置保持运行" }
func (*LoadRejected) Definitive() bool { return true }

var _ application.CaddyPort = (*Client)(nil)

func NewClient(options Options) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if options.AdminURL == "" {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", options.Socket)
		}
	}
	return &Client{options: options, http: &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (client *Client) endpoint(path string) string {
	if client.options.AdminURL != "" {
		return strings.TrimRight(client.options.AdminURL, "/") + path
	}
	return "http://caddy" + path
}

func (client *Client) Read(ctx context.Context) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint("/config/"), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid Caddy Admin API address")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Caddy Admin API 不可达")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Caddy 状态读取失败 (%d)", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil || domain.Fingerprint(raw) == "" {
		return nil, fmt.Errorf("Caddy 返回无效配置")
	}
	return raw, nil
}

func (client *Client) Load(ctx context.Context, raw []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint("/load"), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("invalid Caddy Admin API address")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("Caddy 加载响应丢失，需核对实际状态")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if client.options.AdminURL != "" {
			return fmt.Errorf("外部 Caddy 加载响应异常 (%d)，需核对实际状态", response.StatusCode)
		}
		return &LoadRejected{}
	}
	return nil
}

func (client *Client) Validate(ctx context.Context, raw []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "caddy-validate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	var candidate map[string]json.RawMessage
	if err = json.Unmarshal(raw, &candidate); err != nil {
		return domain.Invalid("无效配置")
	}
	if client.options.AdminURL != "" {
		delete(candidate, "admin")
	}
	candidate["storage"], err = json.Marshal(map[string]string{"module": "file_system", "root": filepath.Join(dir, "storage")})
	if err != nil {
		return err
	}
	isolated, err := json.Marshal(candidate)
	if err != nil {
		return err
	}

	command := exec.CommandContext(ctx, client.options.CaddyBinary, "validate", "--config", "-")
	command.Stdin = bytes.NewReader(isolated)
	command.Env = append(os.Environ(), "XDG_DATA_HOME="+dir, "XDG_CONFIG_HOME="+dir)
	if client.options.AdminURL == "" {
		path := config.CloudflareTokenPath(client.options.DataDir)
		tokenBytes, readErr := os.ReadFile(path)
		token := ""
		if readErr == nil {
			token = strings.TrimSpace(string(tokenBytes))
		}
		if token == "" {
			token = os.Getenv("CLOUDFLARE_API_TOKEN")
		}
		if token != "" {
			command.Env = setEnvironment(command.Env, "CLOUDFLARE_API_TOKEN", token)
		}
	}
	if output, runErr := command.CombinedOutput(); runErr != nil {
		_ = output
		if ctx.Err() != nil {
			return domain.Invalid("Caddy 校验超时")
		}
		return domain.Invalid("Caddy 配置校验失败，请检查模块、部署密钥及上游配置")
	}
	return nil
}

func setEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	for index, current := range environment {
		if strings.HasPrefix(current, prefix) {
			environment[index] = prefix + value
			return environment
		}
	}
	return append(environment, prefix+value)
}

func (client *Client) BuildInfo(ctx context.Context) (string, bool) {
	if client.options.AdminURL != "" {
		return "unknown (external)", false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, client.options.CaddyBinary, "version").Output()
	if err != nil {
		return "unknown", false
	}
	modules, err := exec.CommandContext(ctx, client.options.CaddyBinary, "list-modules").Output()
	return strings.TrimSpace(string(version)), err == nil && strings.Contains(string(modules), "dns.providers.cloudflare")
}
